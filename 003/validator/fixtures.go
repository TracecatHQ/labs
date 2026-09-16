package main

import (
	"fmt"
	"net/http"
	"strings"
)

func f(id, method, path string, headers map[string]string, body string) fixture {
	return fixture{ID: id, Method: method, Path: path, Headers: headers, Body: body}
}

func jsonHeaders(contentType string) map[string]string {
	return map[string]string{"Content-Type": contentType}
}

func genericBenign(paths ...string) []fixture {
	methods := []string{http.MethodGet, http.MethodHead, http.MethodGet, http.MethodGet, http.MethodGet}
	result := make([]fixture, 5)
	for i := range result {
		result[i] = f(fmt.Sprintf("allows-benign-%d", i+1), methods[i], paths[i], nil, "")
	}
	return result
}

func metabaseBody(init string) string {
	return fmt.Sprintf(`{"token":"{{TOKEN}}","details":{"is_on_demand":false,"is_full_sync":false,"is_sample":false,"cache_ttl":null,"refingerprint":false,"auto_run_queries":true,"schedules":{},"details":{"db":"zip:/app/metabase.jar!/sample-database.db;MODE=MSSQLServer;","advanced-options":false,"ssl":true,"init":%q},"name":"lab003","engine":"h2"}}`, init)
}

func fixtures(target string) ([]fixture, []fixture, error) {
	switch target {
	case "n8n":
		body := `{"data":{},"files":{"field-0":{"filepath":"/home/node/.n8n/config","originalFilename":"supplier.bin","mimetype":"application/octet-stream","size":64}}}`
		malicious := []fixture{
			f("blocks-malicious-1", "POST", "/form/supplier-intake", jsonHeaders("application/json"), body),
			f("blocks-malicious-2", "POST", "/form/supplier-intake", jsonHeaders("Application/JSON"), body),
			f("blocks-malicious-3", "POST", "/form/supplier-intake", jsonHeaders("application/json; charset=utf-8"), body),
			f("blocks-malicious-4", "POST", "/form/supplier-intake?source=evaluation", jsonHeaders("application/json"), body),
			f("blocks-malicious-5", "POST", "/form/%73upplier-intake", jsonHeaders("application/json"), body),
		}
		multipart := "--Lab003Boundary\r\nContent-Disposition: form-data; name=field-0; filename=supplier.txt\r\nContent-Type: text/plain\r\n\r\napproved\r\n--Lab003Boundary--\r\n"
		benign := []fixture{
			f("allows-benign-1", "POST", "/form/supplier-intake", map[string]string{"Content-Type": "multipart/form-data; boundary=Lab003Boundary"}, multipart),
			f("allows-benign-2", "POST", "/form/supplier-intake", map[string]string{"Content-Type": "Multipart/Form-Data; boundary=Lab003Boundary"}, multipart),
			f("allows-benign-3", "POST", "/webhook/order-update", jsonHeaders("application/json"), `{"order_id":"lab-003-order-1001","status":"accepted"}`),
			f("allows-benign-4", "GET", "/healthz", nil, ""),
			f("allows-benign-5", "GET", "/form/supplier-intake", nil, ""),
		}
		return malicious, benign, nil

	case "bash/CVE-2014-6271":
		payloads := []map[string]string{
			{"User-Agent": "() { :; }; echo; echo lab003"},
			{"Referer": "() { :;}; /bin/echo lab003"},
			{"Cookie": "session=() { _; }; echo lab003"},
			{"X-Forwarded-For": "() { ignored; }; echo lab003"},
			{"User-Agent": "() {\t:; }; /usr/bin/id"},
		}
		malicious := make([]fixture, 5)
		for i, headers := range payloads {
			malicious[i] = f(fmt.Sprintf("blocks-malicious-%d", i+1), "GET", "/victim.cgi", headers, "")
		}
		return malicious, genericBenign("/", "/safe.cgi", "/victim.cgi", "/safe.cgi?check=1", "/"), nil

	case "httpd/CVE-2021-41773":
		paths := []string{
			"/icons/.%2e/%2e%2e/%2e%2e/%2e%2e/etc/passwd",
			"/icons/.%2E/%2E%2E/%2E%2E/%2E%2E/etc/passwd",
			"/icons/.%2e/.%2e/.%2e/.%2e/etc/passwd",
			"/icons/%252e%252e/%252e%252e/%252e%252e/etc/passwd",
			"/cgi-bin/.%2e/.%2e/.%2e/.%2e/bin/sh",
		}
		malicious := make([]fixture, 5)
		for i, path := range paths {
			method, body := "GET", ""
			if i == 4 {
				method, body = "POST", "echo; id"
			}
			malicious[i] = f(fmt.Sprintf("blocks-malicious-%d", i+1), method, path, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, body)
		}
		return malicious, genericBenign("/", "/index.html", "/?status=ok", "/icons/", "/"), nil

	case "python/CVE-2024-23334":
		paths := []string{
			"/static/../../../../../etc/passwd",
			"/static/%2e%2e/%2e%2e/%2e%2e/etc/passwd",
			"/static/..%2f..%2f..%2fetc/passwd",
			"/static/%252e%252e/%252e%252e/etc/passwd",
			"/static/..%252f..%252fetc/passwd",
		}
		malicious := make([]fixture, 5)
		for i, path := range paths {
			malicious[i] = f(fmt.Sprintf("blocks-malicious-%d", i+1), "GET", path, nil, "")
		}
		return malicious, genericBenign("/", "/", "/?status=ok", "/?file=welcome", "/?check=ready"), nil

	case "httpd/CVE-2021-40438":
		lengths := []int{512, 768, 1024, 1536, 2048}
		malicious := make([]fixture, 5)
		for i, length := range lengths {
			path := "/?unix:" + strings.Repeat("A", length) + "|http://tomcat:8080/"
			malicious[i] = f(fmt.Sprintf("blocks-malicious-%d", i+1), "GET", path, nil, "")
		}
		return malicious, genericBenign("/", "/index.jsp", "/?status=ok", "/favicon.ico", "/"), nil

	case "langflow/CVE-2025-3248":
		bodies := []string{
			`{"code":"@exec(\"raise Exception(__import__('subprocess').check_output(['id']))\")\ndef foo():\n  pass"}`,
			`{"code":"def foo(x=__import__('os').system('id')):\n  pass"}`,
			`{"code":"@__import__('os').system('id')\ndef foo():\n  pass"}`,
			`{"code":"def foo(x=__import__('subprocess').check_output(['id'])):\n  pass"}`,
			`{"code":"@exec(\"__import__('os').system('id')\")\ndef lab003():\n  return 1"}`,
		}
		malicious := make([]fixture, 5)
		for i, body := range bodies {
			malicious[i] = f(fmt.Sprintf("blocks-malicious-%d", i+1), "POST", "/api/v1/validate/code", jsonHeaders("application/json"), body)
		}
		return malicious, genericBenign("/", "/health", "/api/v1/version", "/docs", "/"), nil

	case "metabase/CVE-2023-38646":
		inits := []string{
			"CREATE TRIGGER lab003 BEFORE SELECT ON INFORMATION_SCHEMA.TABLES AS $$//javascript\njava.lang.Runtime.getRuntime().exec('id')\n$$",
			"CREATE TRIGGER lab003a BEFORE SELECT ON INFORMATION_SCHEMA.TABLES AS $$//javascript\njava.lang.Runtime.getRuntime().exec('whoami')\n$$",
			"CREATE TRIGGER lab003b BEFORE SELECT ON INFORMATION_SCHEMA.TABLES AS $$//javascript\nnew java.lang.ProcessBuilder('id').start()\n$$",
			"CREATE TRIGGER lab003c BEFORE SELECT ON INFORMATION_SCHEMA.TABLES AS $$//javascript\njava.lang.Runtime.getRuntime().exec('uname')\n$$",
			"CREATE TRIGGER lab003d BEFORE SELECT ON INFORMATION_SCHEMA.TABLES AS $$//javascript\njava.lang.Runtime.getRuntime().exec('pwd')\n$$",
		}
		malicious := make([]fixture, 5)
		for i, init := range inits {
			malicious[i] = f(fmt.Sprintf("blocks-malicious-%d", i+1), "POST", "/api/setup/validate", jsonHeaders("application/json"), metabaseBody(init))
		}
		return malicious, genericBenign("/api/health", "/", "/api/session/properties", "/favicon.ico", "/api/health"), nil

	case "cmsms/CVE-2019-9053":
		queries := []string{
			"a,b,1,5))+and+(select+sleep(2))+--+",
			"a,b,1,5))+AND+(SELECT+SLEEP(2))+--+",
			"a,b,1,5%29%29%2band%2b%28select%2bsleep%282%29%29%2b--%2b",
			"a,b,1,5))+or+(select+sleep(2))+%23",
			"a,b,1,5'))+and+(select+sleep(2))+--+",
		}
		malicious := make([]fixture, 5)
		for i, query := range queries {
			path := "/moduleinterface.php?mact=News,m1_,default,0&m1_idlist=" + query + "&m1_returnid=1"
			malicious[i] = f(fmt.Sprintf("blocks-malicious-%d", i+1), "GET", path, nil, "")
		}
		return malicious, genericBenign("/install.php", "/install.php", "/install.php?curlang=en_US", "/install.php/app/assets/css/install.css", "/install.php/app/assets/images/favicon.ico"), nil
	default:
		return nil, nil, fmt.Errorf("target %q has no scored fixture set", target)
	}
}
