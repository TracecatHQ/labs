package grading

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const AbstentionLabel = "__abstain__"

var ScoreCSVColumns = []string{
	"schema_version", "lab_id", "evaluation_run_id", "trial_id", "case_id", "trial_number",
	"candidate_session_id", "scoring_run_execution_id", "scoring_attempt_id", "judge_session_id",
	"profile_id", "profile_version", "scorer_kind", "scorer_workflow_alias", "criterion_id",
	"criterion_type", "criterion_weight", "criterion_value", "criterion_points", "criterion_hard_gate",
	"criterion_passed", "trial_hard_failed", "trial_score", "reason", "evidence_refs",
	"candidate_completed_at", "scored_at",
}

var MetricCSVColumns = []string{
	"schema_version", "lab_id", "evaluation_run_id", "trial_id", "case_id", "trial_number",
	"scoring_run_execution_id", "scoring_attempt_id", "profile_id", "profile_version", "metric_id",
	"metric_type", "value", "expected_value", "scored_at",
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

type Number float64

func (n *Number) UnmarshalJSON(data []byte) error {
	text := string(data)
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		text = value
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("invalid numeric value %q: %w", text, err)
	}
	*n = Number(value)
	return nil
}

func (n Number) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(n), 'f', -1, 64)), nil
}

type Scorer struct {
	Kind          string `json:"kind"`
	WorkflowAlias string `json:"workflow_alias"`
	JudgePreset   string `json:"judge_preset,omitempty"`
}

type NumericRange struct {
	Min Number `json:"min"`
	Max Number `json:"max"`
}

type Criterion struct {
	CriterionID      string        `json:"criterion_id"`
	Label            string        `json:"label"`
	Type             string        `json:"type"`
	Weight           Number        `json:"weight"`
	HardGate         bool          `json:"hard_gate"`
	PassThreshold    Number        `json:"pass_threshold"`
	Range            *NumericRange `json:"range,omitempty"`
	JudgeInstruction string        `json:"judge_instruction,omitempty"`
}

type MetricDefinition struct {
	MetricID          string   `json:"metric_id"`
	Label             string   `json:"label"`
	Type              string   `json:"type"`
	SourceCriterionID string   `json:"source_criterion_id,omitempty"`
	Labels            []string `json:"labels,omitempty"`
}

type ScoringProfile struct {
	SchemaVersion  int                `json:"schema_version"`
	ProfileID      string             `json:"profile_id"`
	ProfileVersion int                `json:"profile_version"`
	Scorer         Scorer             `json:"scorer"`
	Criteria       []Criterion        `json:"criteria"`
	Metrics        []MetricDefinition `json:"metrics"`
}

// Oracle is intentionally generic: benchmark-native nested objects remain
// available to profile-selected scorer workflows without a shared schema.
type Oracle map[string]any

type CaseTag struct {
	Name string `json:"name"`
}

type Submission struct {
	Case struct {
		Tags    []CaseTag `json:"tags"`
		Payload struct {
			TargetID string `json:"target_id"`
		} `json:"payload"`
	} `json:"case"`
	Comments []any `json:"comments"`
}

type EvidenceEvent struct {
	Sequence     int    `json:"sequence"`
	EvidenceType string `json:"evidence_type"`
	Evidence     any    `json:"evidence"`
}

type EvidenceRow struct {
	EvidenceKey     string `json:"evidence_key"`
	SchemaVersion   int    `json:"schema_version"`
	LabID           string `json:"lab_id"`
	EvaluationRunID string `json:"evaluation_run_id"`
	TrialID         string `json:"trial_id"`
	Sequence        int    `json:"sequence"`
	EvidenceType    string `json:"evidence_type"`
	Evidence        any    `json:"evidence"`
}

// Trial is the immutable public Candidate snapshot. It intentionally excludes
// private model reasoning and retains only the explicit final answer, visible
// Case/comments, and platform-captured tool evidence stored by sequence.
type Trial struct {
	TrialID                      string         `json:"trial_id"`
	CaseID                       string         `json:"case_id"`
	TrialNumber                  int            `json:"trial_number"`
	Status                       string         `json:"status"`
	CandidateSessionID           string         `json:"candidate_session_id"`
	CandidateAgentLatencySeconds *Number        `json:"candidate_agent_latency_seconds,omitempty"`
	CandidateCompletedAt         string         `json:"candidate_completed_at"`
	FinalAnswer                  any            `json:"final_answer"`
	Submission                   Submission     `json:"submission"`
	Oracle                       Oracle         `json:"oracle"`
	Profile                      ScoringProfile `json:"profile"`
}

type EvaluationRunRow struct {
	SchemaVersion       int            `json:"schema_version"`
	EvaluationRunID     string         `json:"evaluation_run_id"`
	LabID               string         `json:"lab_id"`
	ProfileID           string         `json:"profile_id"`
	ProfileVersion      int            `json:"profile_version"`
	Profile             ScoringProfile `json:"profile"`
	SelectedCaseIDs     []string       `json:"selected_case_ids"`
	TemplateDigest      string         `json:"template_digest"`
	Status              string         `json:"status"`
	ExpectedTrialCount  int            `json:"expected_trial_count"`
	CompletedTrialCount int            `json:"completed_trial_count"`
	BatchSize           int            `json:"batch_size"`
	CreatedAt           string         `json:"created_at"`
	UpdatedAt           string         `json:"updated_at"`
}

type TrialRow struct {
	TrialKey        string `json:"trial_key"`
	SchemaVersion   int    `json:"schema_version"`
	LabID           string `json:"lab_id"`
	EvaluationRunID string `json:"evaluation_run_id"`
	TrialID         string `json:"trial_id"`
	CaseID          string `json:"case_id"`
	TrialNumber     int    `json:"trial_number"`
	Status          string `json:"status"`
	Trial           Trial  `json:"trial"`
}

type ScoringAttempt struct {
	AttemptKey            string  `json:"attempt_key"`
	SchemaVersion         int     `json:"schema_version"`
	LabID                 string  `json:"lab_id"`
	EvaluationRunID       string  `json:"evaluation_run_id"`
	TrialID               string  `json:"trial_id"`
	ScoringRunExecutionID string  `json:"scoring_run_execution_id"`
	ScoringAttemptID      string  `json:"scoring_attempt_id"`
	ScorerKind            string  `json:"scorer_kind"`
	ScorerWorkflowAlias   string  `json:"scorer_workflow_alias"`
	JudgeSessionID        string  `json:"judge_session_id,omitempty"`
	Status                string  `json:"status"`
	Error                 *string `json:"error,omitempty"`
	StartedAt             string  `json:"started_at"`
	CompletedAt           string  `json:"completed_at,omitempty"`
	ScorerLatencySeconds  *Number `json:"scorer_latency_seconds,omitempty"`
}

type ScoreRow struct {
	ScoreKey              string   `json:"score_key"`
	SchemaVersion         int      `json:"schema_version"`
	LabID                 string   `json:"lab_id"`
	EvaluationRunID       string   `json:"evaluation_run_id"`
	TrialID               string   `json:"trial_id"`
	CaseID                string   `json:"case_id"`
	TrialNumber           int      `json:"trial_number"`
	CandidateSessionID    string   `json:"candidate_session_id"`
	ScoringRunExecutionID string   `json:"scoring_run_execution_id"`
	ScoringAttemptID      string   `json:"scoring_attempt_id"`
	JudgeSessionID        string   `json:"judge_session_id,omitempty"`
	ProfileID             string   `json:"profile_id"`
	ProfileVersion        int      `json:"profile_version"`
	ScorerKind            string   `json:"scorer_kind"`
	ScorerWorkflowAlias   string   `json:"scorer_workflow_alias"`
	CriterionID           string   `json:"criterion_id"`
	CriterionType         string   `json:"criterion_type"`
	CriterionWeight       Number   `json:"criterion_weight"`
	CriterionValue        Number   `json:"criterion_value"`
	CriterionPoints       Number   `json:"criterion_points"`
	CriterionHardGate     bool     `json:"criterion_hard_gate"`
	CriterionPassed       bool     `json:"criterion_passed"`
	TrialHardFailed       bool     `json:"trial_hard_failed"`
	TrialScore            Number   `json:"trial_score"`
	Reason                string   `json:"reason"`
	EvidenceRefs          []string `json:"evidence_refs"`
	CandidateCompletedAt  string   `json:"candidate_completed_at"`
	ScoredAt              string   `json:"scored_at"`
}

type MetricRow struct {
	MetricKey             string          `json:"metric_key"`
	SchemaVersion         int             `json:"schema_version"`
	LabID                 string          `json:"lab_id"`
	EvaluationRunID       string          `json:"evaluation_run_id"`
	TrialID               string          `json:"trial_id"`
	CaseID                string          `json:"case_id"`
	TrialNumber           int             `json:"trial_number"`
	ScoringRunExecutionID string          `json:"scoring_run_execution_id"`
	ScoringAttemptID      string          `json:"scoring_attempt_id"`
	ProfileID             string          `json:"profile_id"`
	ProfileVersion        int             `json:"profile_version"`
	MetricID              string          `json:"metric_id"`
	MetricType            string          `json:"metric_type"`
	Value                 json.RawMessage `json:"value"`
	ExpectedValue         json.RawMessage `json:"expected_value"`
	ScoredAt              string          `json:"scored_at"`
}

type Execution struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	StartTime string `json:"start_time"`
	CloseTime string `json:"close_time"`
}

func (e Execution) duration() (float64, error) {
	start, err := time.Parse(time.RFC3339Nano, e.StartTime)
	if err != nil {
		return 0, err
	}
	closeTime, err := time.Parse(time.RFC3339Nano, e.CloseTime)
	if err != nil {
		return 0, err
	}
	return closeTime.Sub(start).Seconds(), nil
}

type Inputs struct {
	LabID              string
	RunID              string
	RunRows            []EvaluationRunRow
	Trials             []TrialRow
	Evidence           []EvidenceRow
	Scores             []ScoreRow
	Metrics            []MetricRow
	Attempts           []ScoringAttempt
	CandidateExecution Execution
	ScoringExecutions  []Execution
}

type ScoreDistribution struct {
	Score float64 `json:"score"`
	Count int     `json:"count"`
}

type TrialSummary struct {
	Count             int                 `json:"count"`
	MeanScore         float64             `json:"mean_score"`
	MedianScore       float64             `json:"median_score"`
	MinScore          float64             `json:"min_score"`
	MaxScore          float64             `json:"max_score"`
	HardGatePassRate  *float64            `json:"hard_gate_pass_rate"`
	ScoreDistribution []ScoreDistribution `json:"score_distribution"`
}

type CriterionAggregate struct {
	MeanValue  float64  `json:"mean_value"`
	MeanPoints float64  `json:"mean_points"`
	PassRate   *float64 `json:"pass_rate"`
}

type PerClassMetrics struct {
	Precision *float64 `json:"precision"`
	Recall    *float64 `json:"recall"`
	F1        *float64 `json:"f1"`
	Support   int      `json:"support"`
}

type ClassificationMetrics struct {
	Support           int                        `json:"support"`
	ConfusionMatrix   map[string]map[string]int  `json:"confusion_matrix"`
	Accuracy          *float64                   `json:"accuracy"`
	BalancedAccuracy  *float64                   `json:"balanced_accuracy"`
	MacroF1           *float64                   `json:"macro_f1"`
	SupportWeightedF1 *float64                   `json:"support_weighted_f1"`
	MCC               *float64                   `json:"mcc"`
	PerClass          map[string]PerClassMetrics `json:"per_class"`
}

type MetricAggregate struct {
	Type           string                 `json:"type"`
	Count          int                    `json:"count"`
	Mean           *float64               `json:"mean,omitempty"`
	StandardError  *float64               `json:"standard_error"`
	Classification *ClassificationMetrics `json:"classification,omitempty"`
}

type WorkflowTiming struct {
	Candidate float64 `json:"candidate"`
	Scoring   float64 `json:"scoring"`
}

type StatusSummary struct {
	Run             string         `json:"run"`
	Trials          map[string]int `json:"trials"`
	ScoringAttempts map[string]int `json:"scoring_attempts"`
}

type ProvenanceSummary struct {
	ScorerKind          string   `json:"scorer_kind"`
	ScorerWorkflowAlias string   `json:"scorer_workflow_alias"`
	ScoringExecutionIDs []string `json:"scoring_execution_ids"`
}

type ProfileSummary struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type Summary struct {
	SchemaVersion   int                           `json:"schema_version"`
	LabID           string                        `json:"lab_id"`
	EvaluationRunID string                        `json:"evaluation_run_id"`
	TargetID        *string                       `json:"target_id"`
	TargetIDs       []string                      `json:"target_ids"`
	Tags            []string                      `json:"tags"`
	Profile         ProfileSummary                `json:"profile"`
	Status          StatusSummary                 `json:"status"`
	Provenance      ProvenanceSummary             `json:"provenance"`
	Trials          TrialSummary                  `json:"trials"`
	Criteria        map[string]CriterionAggregate `json:"criteria"`
	Metrics         map[string]MetricAggregate    `json:"metrics"`
	WorkflowTiming  WorkflowTiming                `json:"workflow_wall_time_seconds"`
}

type labelPair struct {
	Expected  string
	Predicted *string
}
