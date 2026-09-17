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
	fixtureRunID     = "candidate/exec"
	fixtureScoringID = "judge/exec"
)

func TestNumericAndBinaryCriteriaAggregate(t *testing.T) {
	inputs := fixtureInputs()
	report, err := NewReport(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Summary().Trials.MeanScore; math.Abs(got-75) > epsilon {
		t.Fatalf("mean score = %v", got)
	}
	if got := report.Summary().Criteria["quality"].MeanValue; math.Abs(got-7.5) > epsilon {
		t.Fatalf("mean numeric value = %v", got)
	}
	if got := report.Summary().Provenance.ScorerKind; got != "hybrid" {
		t.Fatalf("scorer kind = %q", got)
	}
}

func TestHardGateZerosTrialTotal(t *testing.T) {
	inputs := fixtureInputs()
	for i := range inputs.Scores {
		if inputs.Scores[i].TrialID == "two" {
			inputs.Scores[i].TrialHardFailed = true
			inputs.Scores[i].TrialScore = 0
			if inputs.Scores[i].CriterionID == "gate" {
				inputs.Scores[i].CriterionValue = 0
				inputs.Scores[i].CriterionPassed = false
			}
		}
	}
	report, err := NewReport(inputs)
	if err != nil {
		t.Fatal(err)
	}
	assertFloat(t, report.Summary().Trials.HardGatePassRate, 0.5)
	if report.Summary().Trials.MinScore != 0 {
		t.Fatalf("min score = %v", report.Summary().Trials.MinScore)
	}
}

func TestClassificationMetricAndAbstention(t *testing.T) {
	inputs := fixtureInputs()
	inputs.Metrics[1].Value = json.RawMessage(`null`)
	report, err := NewReport(inputs)
	if err != nil {
		t.Fatal(err)
	}
	metric := report.Summary().Metrics["verdict"].Classification
	assertFloat(t, metric.Accuracy, 0.5)
	if metric.ConfusionMatrix["bad"][AbstentionLabel] != 1 {
		t.Fatalf("confusion matrix = %#v", metric.ConfusionMatrix)
	}
}

func TestNumericMetricIncludesSampleStandardError(t *testing.T) {
	inputs := fixtureInputs()
	definition := MetricDefinition{MetricID: "quality_value", Label: "quality value", Type: "numeric"}
	inputs.RunRows[0].Profile.Metrics = append(inputs.RunRows[0].Profile.Metrics, definition)
	for i := range inputs.Trials {
		inputs.Trials[i].Trial.Profile = inputs.RunRows[0].Profile
		value := 2 + i*2
		inputs.Metrics = append(inputs.Metrics, MetricRow{MetricKey: fixtureRunID + ":" + inputs.Trials[i].TrialID + ":quality_value", SchemaVersion: 2, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: inputs.Trials[i].TrialID, CaseID: inputs.Trials[i].CaseID, TrialNumber: 1, ScoringRunExecutionID: fixtureScoringID, ScoringAttemptID: "attempt-" + inputs.Trials[i].TrialID, ProfileID: "fixture", ProfileVersion: 1, MetricID: "quality_value", MetricType: "numeric", Value: json.RawMessage(number(float64(value))), ExpectedValue: json.RawMessage(`null`), ScoredAt: "2026-01-01T00:02:00Z"})
	}
	report, err := NewReport(inputs)
	if err != nil {
		t.Fatal(err)
	}
	aggregate := report.Summary().Metrics["quality_value"]
	assertFloat(t, aggregate.Mean, 3)
	assertFloat(t, aggregate.StandardError, 1)
}

func TestNumericOutOfRangeRejected(t *testing.T) {
	inputs := fixtureInputs()
	for i := range inputs.Scores {
		if inputs.Scores[i].CriterionID == "quality" {
			inputs.Scores[i].CriterionValue = 11
			break
		}
	}
	assertErrorContains(t, inputs, "outside the criterion range")
}

func TestIncompleteMetricCoverageRejected(t *testing.T) {
	inputs := fixtureInputs()
	inputs.Metrics = inputs.Metrics[:1]
	assertErrorContains(t, inputs, "metric coverage is incomplete")
}

func TestUnorderedFrozenEvidenceRejected(t *testing.T) {
	inputs := fixtureInputs()
	inputs.Evidence[1].Sequence = 9
	inputs.Evidence[1].EvidenceKey = fixtureRunID + ":one:9"
	assertErrorContains(t, inputs, "evidence is not ordered")
}

func TestReportWritesScoresMetricsAndSummary(t *testing.T) {
	report, err := NewReport(fixtureInputs())
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := report.Write(directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scores.csv", "metrics.csv", "summary.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProfileRejectsDuplicateMetrics(t *testing.T) {
	inputs := fixtureInputs()
	inputs.RunRows[0].Profile.Metrics = append(inputs.RunRows[0].Profile.Metrics, inputs.RunRows[0].Profile.Metrics[0])
	for i := range inputs.Trials {
		inputs.Trials[i].Trial.Profile = inputs.RunRows[0].Profile
	}
	assertErrorContains(t, inputs, "invalid metric")
}

func TestCompletedDurableRunCanOutliveFailedOriginalExecution(t *testing.T) {
	inputs := fixtureInputs()
	inputs.CandidateExecution.Status = "FAILED"
	inputs.CandidateExecution.CloseTime = "2026-01-01T00:00:30Z"
	if _, err := NewReport(inputs); err != nil {
		t.Fatalf("resumed durable run rejected: %v", err)
	}
}

func TestFrozenCaseSelectionMismatchRejected(t *testing.T) {
	inputs := fixtureInputs()
	inputs.RunRows[0].SelectedCaseIDs[1] = "different-case"
	assertErrorContains(t, inputs, "frozen Case selection")
}

func TestScoringResumePreservesPerTrialProvenance(t *testing.T) {
	inputs := fixtureInputs()
	inputs.ScoringExecutions[0].Status = "FAILED"
	const resumed = "judge/resumed"
	for i := range inputs.Scores {
		if inputs.Scores[i].TrialID == "two" {
			inputs.Scores[i].ScoringRunExecutionID = resumed
			inputs.Scores[i].ScoringAttemptID = "resumed-two"
		}
	}
	inputs.Metrics[1].ScoringRunExecutionID = resumed
	inputs.Metrics[1].ScoringAttemptID = "resumed-two"
	inputs.Attempts[1].ScoringRunExecutionID = resumed
	inputs.Attempts[1].ScoringAttemptID = "resumed-two"
	inputs.ScoringExecutions = append(inputs.ScoringExecutions, Execution{ID: resumed, Status: "COMPLETED", StartTime: "2026-01-01T00:03:00Z", CloseTime: "2026-01-01T00:04:00Z"})
	report, err := NewReport(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Summary().Provenance.ScoringExecutionIDs) != 2 {
		t.Fatalf("scoring executions = %#v", report.Summary().Provenance.ScoringExecutionIDs)
	}
}

func assertErrorContains(t *testing.T, inputs Inputs, substring string) {
	t.Helper()
	_, err := NewReport(inputs)
	if err == nil || !strings.Contains(err.Error(), substring) {
		t.Fatalf("error = %v, want %q", err, substring)
	}
}

func assertFloat(t *testing.T, value *float64, expected float64) {
	t.Helper()
	if value == nil || math.Abs(*value-expected) > epsilon {
		t.Fatalf("value = %v, want %v", value, expected)
	}
}

func fixtureInputs() Inputs {
	profile := ScoringProfile{
		SchemaVersion: 2, ProfileID: "fixture", ProfileVersion: 1,
		Scorer: Scorer{Kind: "hybrid", WorkflowAlias: "fixture_scorer", JudgePreset: "judge"},
		Criteria: []Criterion{
			{CriterionID: "gate", Label: "gate", Type: "binary", HardGate: true, PassThreshold: 1},
			{CriterionID: "quality", Label: "quality", Type: "numeric", Weight: 100, PassThreshold: 5, Range: &NumericRange{Min: 0, Max: 10}},
		},
		Metrics: []MetricDefinition{{MetricID: "verdict", Label: "verdict", Type: "classification", SourceCriterionID: "quality", Labels: []string{"good", "bad"}}},
	}
	inputs := Inputs{
		LabID: "999", RunID: fixtureRunID,
		RunRows:            []EvaluationRunRow{{SchemaVersion: 2, EvaluationRunID: fixtureRunID, LabID: "999", ProfileID: profile.ProfileID, ProfileVersion: 1, Profile: profile, SelectedCaseIDs: []string{"case-one", "case-two"}, TemplateDigest: "fixture-digest", Status: "completed", ExpectedTrialCount: 2, CompletedTrialCount: 2, BatchSize: 4}},
		CandidateExecution: Execution{ID: fixtureRunID, Status: "COMPLETED", StartTime: "2026-01-01T00:00:00Z", CloseTime: "2026-01-01T00:01:00Z"},
		ScoringExecutions:  []Execution{{ID: fixtureScoringID, Status: "COMPLETED", StartTime: "2026-01-01T00:01:00Z", CloseTime: "2026-01-01T00:02:00Z"}},
	}
	for index, id := range []string{"one", "two"} {
		value := Number(5 + index*5)
		trial := Trial{TrialID: id, CaseID: "case-" + id, TrialNumber: 1, Status: "completed", CandidateSessionID: "candidate-" + id, CandidateCompletedAt: "2026-01-01T00:01:00Z", FinalAnswer: "answer", Profile: profile, Oracle: Oracle{"criteria": map[string]any{"gate": map[string]any{"expected": true}, "quality": map[string]any{"expected": 10}}}}
		inputs.Trials = append(inputs.Trials, TrialRow{TrialKey: fixtureRunID + ":" + id, SchemaVersion: 2, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: id, CaseID: trial.CaseID, TrialNumber: 1, Status: "completed", Trial: trial})
		for sequence, event := range []struct {
			kind  string
			value any
		}{{"final_answer", "answer"}, {"case", map[string]any{}}, {"comments", []any{}}} {
			inputs.Evidence = append(inputs.Evidence, EvidenceRow{EvidenceKey: fixtureRunID + ":" + id + ":" + string(rune('0'+sequence)), SchemaVersion: 2, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: id, Sequence: sequence, EvidenceType: event.kind, Evidence: event.value})
		}
		for _, criterion := range profile.Criteria {
			criterionValue, points := Number(1), Number(0)
			if criterion.CriterionID == "quality" {
				criterionValue, points = value, value*10
			}
			inputs.Scores = append(inputs.Scores, ScoreRow{ScoreKey: fixtureRunID + ":" + id + ":" + criterion.CriterionID, SchemaVersion: 2, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: id, CaseID: trial.CaseID, TrialNumber: 1, CandidateSessionID: trial.CandidateSessionID, ScoringRunExecutionID: fixtureScoringID, ScoringAttemptID: "attempt-" + id, JudgeSessionID: "judge-" + id, ProfileID: profile.ProfileID, ProfileVersion: 1, ScorerKind: "hybrid", ScorerWorkflowAlias: "fixture_scorer", CriterionID: criterion.CriterionID, CriterionType: criterion.Type, CriterionWeight: criterion.Weight, CriterionValue: criterionValue, CriterionPoints: points, CriterionHardGate: criterion.HardGate, CriterionPassed: true, TrialScore: value * 10, Reason: "fixture", EvidenceRefs: []string{}, CandidateCompletedAt: trial.CandidateCompletedAt, ScoredAt: "2026-01-01T00:02:00Z"})
		}
		label := `"good"`
		expected := `"good"`
		if id == "two" {
			label, expected = `"bad"`, `"bad"`
		}
		inputs.Metrics = append(inputs.Metrics, MetricRow{MetricKey: fixtureRunID + ":" + id + ":verdict", SchemaVersion: 2, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: id, CaseID: trial.CaseID, TrialNumber: 1, ScoringRunExecutionID: fixtureScoringID, ScoringAttemptID: "attempt-" + id, ProfileID: profile.ProfileID, ProfileVersion: 1, MetricID: "verdict", MetricType: "classification", Value: json.RawMessage(label), ExpectedValue: json.RawMessage(expected), ScoredAt: "2026-01-01T00:02:00Z"})
		latency := Number(1)
		inputs.Attempts = append(inputs.Attempts, ScoringAttempt{AttemptKey: fixtureRunID + ":" + id, SchemaVersion: 2, LabID: "999", EvaluationRunID: fixtureRunID, TrialID: id, ScoringRunExecutionID: fixtureScoringID, ScoringAttemptID: "attempt-" + id, ScorerKind: "hybrid", ScorerWorkflowAlias: "fixture_scorer", JudgeSessionID: "judge-" + id, Status: "completed", StartedAt: "2026-01-01T00:01:00Z", CompletedAt: "2026-01-01T00:02:00Z", ScorerLatencySeconds: &latency})
	}
	return inputs
}
