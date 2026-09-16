package grading

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fixtureRunID   = "candidate/exec"
	fixtureJudgeID = "judge/exec"
)

type trialSpec struct {
	id                                    string
	expectedKind, predictedKind           *string
	expectedRelevance, predictedRelevance *string
	gateMet                               bool
}

func TestPerfectImbalancedClassification(t *testing.T) {
	report := mustReport(t, []trialSpec{
		spec("a1", "a", "a", "x", "x", true),
		spec("a2", "a", "a", "x", "x", true),
		spec("a3", "a", "a", "y", "y", true),
		spec("b1", "b", "b", "x", "x", true),
	}, false)
	metrics := report.Summary().Classification.Criteria["kind"]
	assertFloat(t, metrics.Accuracy, 1)
	assertFloat(t, metrics.BalancedAccuracy, 1)
	assertFloat(t, metrics.MacroF1, 1)
	if got := metrics.ConfusionMatrix["a"]; got["a"] != 3 || got["b"] != 0 || got[AbstentionLabel] != 0 {
		t.Fatalf("unexpected confusion row: %#v", got)
	}
}

func TestMinorityFailureIsValidLowScore(t *testing.T) {
	report := mustReport(t, []trialSpec{
		spec("a1", "a", "a", "x", "x", true),
		spec("a2", "a", "a", "x", "x", true),
		spec("b1", "b", "a", "x", "x", true),
	}, false)
	metrics := report.Summary().Classification.Criteria["kind"]
	assertFloat(t, metrics.Accuracy, 2.0/3)
	assertFloat(t, metrics.PerClass["b"].Recall, 0)
	if metrics.PerClass["b"].Precision != nil {
		t.Fatal("minority precision should be undefined")
	}
	assertFloat(t, metrics.PerClass["b"].F1, 0)
	if got := report.Summary().Trials.MeanScore; math.Abs(got-250.0/3) > epsilon {
		t.Fatalf("mean score = %v", got)
	}
}

func TestAbstentionCountsAsIncorrect(t *testing.T) {
	report := mustReport(t, []trialSpec{
		specPtr("a1", ptr("a"), nil, ptr("x"), ptr("x"), true),
		spec("b1", "b", "b", "y", "y", true),
	}, false)
	metrics := report.Summary().Classification.Criteria["kind"]
	assertFloat(t, metrics.Accuracy, 0.5)
	if metrics.ConfusionMatrix["a"][AbstentionLabel] != 1 {
		t.Fatalf("abstention not counted: %#v", metrics.ConfusionMatrix)
	}
}

func TestHardGateFailuresAreExcluded(t *testing.T) {
	report := mustReport(t, []trialSpec{
		spec("passed", "a", "a", "x", "x", true),
		spec("failed", "b", "b", "y", "y", false),
	}, false)
	if got := report.Summary().Classification.Criteria["kind"].Support; got != 1 {
		t.Fatalf("classification support = %d", got)
	}
	assertFloat(t, report.Summary().Trials.HardGatePassRate, 0.5)
	if report.Summary().Trials.MinScore != 0 {
		t.Fatalf("minimum score = %v", report.Summary().Trials.MinScore)
	}
}

func TestMultipleOutputsHaveJointExactMatch(t *testing.T) {
	report := mustReport(t, []trialSpec{
		spec("one", "a", "a", "x", "x", true),
		spec("two", "b", "b", "y", "x", true),
	}, false)
	assertFloat(t, report.Summary().Classification.JointExactMatchAccuracy, 0.5)
}

func TestUndefinedJointExactMatchIsExplicitNull(t *testing.T) {
	report := mustReport(t, []trialSpec{
		spec("one", "a", "a", "x", "x", false),
		spec("two", "b", "b", "y", "y", false),
	}, false)
	data, err := json.Marshal(report.Summary())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"joint_exact_match_accuracy":null`) {
		t.Fatalf("summary does not contain explicit null: %s", data)
	}
	if !strings.Contains(report.TerminalReport(), "Joint exact-match accuracy: N/A") {
		t.Fatal("terminal report does not show undefined joint exact match")
	}
}

func TestLegacyResultsHaveGenericReport(t *testing.T) {
	report := mustReport(t, []trialSpec{spec("old", "a", "a", "x", "x", true)}, true)
	if report.Summary().Timing.AgentLatencySeconds != nil {
		t.Fatal("legacy report has agent timing")
	}
	if len(report.Summary().Classification.Criteria) != 0 {
		t.Fatal("legacy report has classification metrics")
	}
	if len(report.Summary().Notices) != 1 || !strings.Contains(report.Summary().Notices[0], "re-judge") {
		t.Fatalf("legacy notice = %#v", report.Summary().Notices)
	}
}

func TestMissingScoreRowIsRejected(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	inputs.Scores = inputs.Scores[:len(inputs.Scores)-1]
	assertErrorContains(t, inputs, "coverage is incomplete")
}

func TestNoScoresIncludeJudgeGuidance(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	inputs.Scores = nil
	assertErrorContains(t, inputs, "just judge 999")
}

func TestDuplicateScoreRowIsRejected(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	inputs.Scores = append(inputs.Scores, inputs.Scores[0])
	assertErrorContains(t, inputs, "duplicate")
}

func TestInvalidScoreArithmeticIsRejected(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	for index := range inputs.Scores {
		inputs.Scores[index].TrialScore = 17
	}
	assertErrorContains(t, inputs, "score arithmetic")
}

func TestIncompleteExecutionIsRejected(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	inputs.CandidateExecution.Status = "RUNNING"
	assertErrorContains(t, inputs, "wait for it to complete")
}

func TestReportWritesCSVAndJSON(t *testing.T) {
	report := mustReport(t, []trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	directory := t.TempDir()
	if err := report.Write(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "scores.csv")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary Summary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Trials.Count != 1 {
		t.Fatalf("trial count = %d", summary.Trials.Count)
	}
}

func mustReport(t *testing.T, specs []trialSpec, legacy bool) *Report {
	t.Helper()
	report, err := NewReport(fixtureInputs(specs, legacy))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func assertErrorContains(t *testing.T, inputs Inputs, substring string) {
	t.Helper()
	_, err := NewReport(inputs)
	if err == nil || !strings.Contains(err.Error(), substring) {
		t.Fatalf("error = %v, want substring %q", err, substring)
	}
}

func assertFloat(t *testing.T, value *float64, expected float64) {
	t.Helper()
	if value == nil || math.Abs(*value-expected) > epsilon {
		t.Fatalf("value = %v, want %v", value, expected)
	}
}

func spec(id, expectedKind, predictedKind, expectedRelevance, predictedRelevance string, gateMet bool) trialSpec {
	return specPtr(id, ptr(expectedKind), ptr(predictedKind), ptr(expectedRelevance), ptr(predictedRelevance), gateMet)
}

func specPtr(id string, expectedKind, predictedKind, expectedRelevance, predictedRelevance *string, gateMet bool) trialSpec {
	return trialSpec{id: id, expectedKind: expectedKind, predictedKind: predictedKind, expectedRelevance: expectedRelevance, predictedRelevance: predictedRelevance, gateMet: gateMet}
}

func ptr(value string) *string { return &value }

func fixtureInputs(specs []trialSpec, legacy bool) Inputs {
	rubric := Rubric{SchemaVersion: 1, RubricID: "fixture-rubric", RubricVersion: 3, Criteria: []Criterion{
		{CriterionID: "gate", Label: "gate", Weight: 0, HardGate: true},
		{CriterionID: "kind", Label: "kind", Weight: 50, Metric: &Metric{Type: "classification", Labels: []string{"a", "b"}}},
		{CriterionID: "relevance", Label: "relevance", Weight: 50, Metric: &Metric{Type: "classification", Labels: []string{"x", "y"}}},
	}}
	trials := make([]Trial, 0, len(specs))
	var scores []ScoreRow
	var details []TrialDetail
	for index, item := range specs {
		latency := Number(index + 1)
		trial := Trial{
			TrialID: item.id, CaseID: "case-" + item.id, TrialNumber: 1, Status: "completed",
			CandidateSessionID: "candidate-session-" + item.id, CandidateAgentLatencySeconds: &latency,
			CandidateCompletedAt: "2026-01-01T00:01:00Z", Rubric: rubric,
			Oracle: Oracle{Criteria: map[string]OracleCriterion{
				"gate": {Expected: "pass"}, "kind": {Expected: *item.expectedKind}, "relevance": {Expected: *item.expectedRelevance},
			}},
		}
		trials = append(trials, trial)
		results := map[string]string{"gate": "met", "kind": "missed", "relevance": "missed"}
		if !item.gateMet {
			results["gate"] = "missed"
		}
		if item.predictedKind != nil && *item.predictedKind == *item.expectedKind {
			results["kind"] = "met"
		}
		if item.predictedRelevance != nil && *item.predictedRelevance == *item.expectedRelevance {
			results["relevance"] = "met"
		}
		points := map[string]Number{"gate": 0}
		if results["kind"] == "met" {
			points["kind"] = 50
		}
		if results["relevance"] == "met" {
			points["relevance"] = 50
		}
		trialScore := points["kind"] + points["relevance"]
		if !item.gateMet {
			trialScore = 0
		}
		for _, criterion := range rubric.Criteria {
			scores = append(scores, ScoreRow{
				SchemaVersion: 1, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: trial.TrialID,
				CaseID: trial.CaseID, TrialNumber: 1, CandidateSessionID: trial.CandidateSessionID,
				JudgeRunExecutionID: fixtureJudgeID, JudgeSessionID: "judge-session-" + item.id,
				RubricID: rubric.RubricID, RubricVersion: rubric.RubricVersion, CriterionID: criterion.CriterionID,
				CriterionWeight: criterion.Weight, CriterionResult: results[criterion.CriterionID], CriterionPoints: points[criterion.CriterionID],
				CriterionHardGate: criterion.HardGate, TrialHardFailed: !item.gateMet, TrialScore: trialScore,
				Reason: "fixture", EvidenceRefs: []string{}, CandidateCompletedAt: trial.CandidateCompletedAt, JudgedAt: "2026-01-01T00:02:00Z",
			})
		}
		if !legacy {
			details = append(details, TrialDetail{
				JudgeRunExecutionID: fixtureJudgeID,
				DetailKey:           fixtureRunID + ":" + item.id, SchemaVersion: 1, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: item.id,
				CandidateAgentLatencySeconds: &latency, JudgeAgentLatencySeconds: Number(index + 2),
				ExpectedLabels:  map[string]string{"kind": *item.expectedKind, "relevance": *item.expectedRelevance},
				PredictedLabels: map[string]*string{"kind": item.predictedKind, "relevance": item.predictedRelevance},
			})
		}
	}
	run := EvaluationRun{SchemaVersion: 1, EvaluationRunID: fixtureRunID, LabID: "999", RubricID: rubric.RubricID, RubricVersion: rubric.RubricVersion, Trials: trials}
	return Inputs{
		LabID: "999", RunID: fixtureRunID, RunRows: []EvaluationRunRow{{EvaluationRunID: fixtureRunID, Run: run}},
		Scores: scores, Details: details,
		CandidateExecution: Execution{ID: fixtureRunID, Status: "COMPLETED", StartTime: "2026-01-01T00:00:00Z", CloseTime: "2026-01-01T00:01:00Z"},
		JudgeExecution:     Execution{ID: fixtureJudgeID, Status: "COMPLETED", StartTime: "2026-01-01T00:01:00Z", CloseTime: "2026-01-01T00:03:00Z"},
	}
}

func TestHistoricalRejudgeWithoutCandidateLatency(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("old", "a", "a", "x", "x", true)}, false)
	inputs.RunRows[0].Run.Trials[0].CandidateAgentLatencySeconds = nil
	inputs.Details[0].CandidateAgentLatencySeconds = nil
	report, err := NewReport(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary().Timing.AgentLatencySeconds.Candidate != nil {
		t.Fatal("missing latency reported as measured")
	}
	assertFloat(t, report.Summary().Classification.Criteria["kind"].Accuracy, 1)
	if !strings.Contains(report.TerminalReport(), "was not captured") {
		t.Fatal("missing notice")
	}
}

func TestMissingCapturedCandidateLatencyRejected(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	inputs.Details[0].CandidateAgentLatencySeconds = nil
	assertErrorContains(t, inputs, "latency does not match")
}

func TestStaleTrialDetailsRejected(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	inputs.Details[0].JudgeRunExecutionID = "judge/previous-execution"
	assertErrorContains(t, inputs, "detail identity is invalid")
}

func TestReservedAbstentionLabelRejected(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	inputs.RunRows[0].Run.Trials[0].Rubric.Criteria[1].Metric.Labels = []string{"a", AbstentionLabel}
	assertErrorContains(t, inputs, "invalid labels")
}

func TestCandidateTimestampComparedAsInstant(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true)}, false)
	for i := range inputs.Scores {
		inputs.Scores[i].CandidateCompletedAt = "2025-12-31T19:01:00.000000-05:00"
	}
	if _, err := NewReport(inputs); err != nil {
		t.Fatal(err)
	}
	inputs.Scores[0].CandidateCompletedAt = "2026-01-01T00:01:01Z"
	assertErrorContains(t, inputs, "completion time does not match")
}

func TestSummaryTagsFromFrozenCases(t *testing.T) {
	inputs := fixtureInputs([]trialSpec{spec("one", "a", "a", "x", "x", true), spec("two", "b", "b", "y", "y", true)}, false)
	inputs.RunRows[0].Run.Trials[0].Submission.Case.Tags = []CaseTag{{Name: "rce"}, {Name: "CVE-2014-6271"}, {Name: ""}}
	inputs.RunRows[0].Run.Trials[1].Submission.Case.Tags = []CaseTag{{Name: "rce"}, {Name: "CVE-2021-41773"}}
	report, err := NewReport(inputs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(report.Summary().Tags)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `["CVE-2014-6271","CVE-2021-41773","rce"]` {
		t.Fatalf("tags = %s", got)
	}
}

func TestSummaryWithoutTagsHasEmptyArray(t *testing.T) {
	report := mustReport(t, []trialSpec{spec("old", "a", "a", "x", "x", true)}, true)
	data, err := json.Marshal(report.Summary())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tags":[]`) {
		t.Fatalf("summary = %s", data)
	}
}

func TestSummaryTargetIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		targets []string
		wantIDs string
		wantID  string
	}{
		{"built-in", []string{"n8n"}, `["n8n"]`, `"n8n"`},
		{"Vulhub", []string{"bash/CVE-2014-6271", "bash/CVE-2014-6271"}, `["bash/CVE-2014-6271"]`, `"bash/CVE-2014-6271"`},
		{"multiple", []string{"python/CVE-2024-23334", "bash/CVE-2014-6271"}, `["bash/CVE-2014-6271","python/CVE-2024-23334"]`, `null`},
		{"historical missing", []string{""}, `[]`, `null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			specs := make([]trialSpec, len(test.targets))
			for i := range specs {
				specs[i] = spec(string(rune('a'+i)), "a", "a", "x", "x", true)
			}
			inputs := fixtureInputs(specs, true)
			for i, target := range test.targets {
				inputs.RunRows[0].Run.Trials[i].Submission.Case.Payload.TargetID = target
			}
			report, err := NewReport(inputs)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(report.Summary())
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["target_id"]) != test.wantID || string(fields["target_ids"]) != test.wantIDs {
				t.Fatalf("target fields = %s / %s", fields["target_id"], fields["target_ids"])
			}
		})
	}
}
