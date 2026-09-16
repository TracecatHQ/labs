package grading

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const AbstentionLabel = "__abstain__"

var CSVColumns = []string{
	"schema_version", "lab_id", "evaluation_run_id", "trial_id", "case_id", "trial_number",
	"candidate_session_id", "judge_run_execution_id", "judge_session_id", "rubric_id",
	"rubric_version", "criterion_id", "criterion_weight", "criterion_result",
	"criterion_points", "criterion_hard_gate", "trial_hard_failed", "trial_score", "reason",
	"evidence_refs", "candidate_completed_at", "judged_at",
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

type Metric struct {
	Type   string   `json:"type"`
	Labels []string `json:"labels"`
}

type Criterion struct {
	CriterionID string  `json:"criterion_id"`
	Label       string  `json:"label"`
	Weight      Number  `json:"weight"`
	HardGate    bool    `json:"hard_gate"`
	Metric      *Metric `json:"metric,omitempty"`
}

type Rubric struct {
	SchemaVersion int         `json:"schema_version"`
	RubricID      string      `json:"rubric_id"`
	RubricVersion int         `json:"rubric_version"`
	Criteria      []Criterion `json:"criteria"`
}

type OracleCriterion struct {
	Expected any `json:"expected"`
}

type Oracle struct {
	Criteria map[string]OracleCriterion `json:"criteria"`
}

// Submission metadata is informational; scoring uses the frozen Oracle and Rubric.
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
}

type Trial struct {
	Submission                   Submission `json:"submission"`
	TrialID                      string     `json:"trial_id"`
	CaseID                       string     `json:"case_id"`
	TrialNumber                  int        `json:"trial_number"`
	Status                       string     `json:"status"`
	CandidateSessionID           string     `json:"candidate_session_id"`
	CandidateAgentLatencySeconds *Number    `json:"candidate_agent_latency_seconds,omitempty"`
	CandidateCompletedAt         string     `json:"candidate_completed_at"`
	Oracle                       Oracle     `json:"oracle"`
	Rubric                       Rubric     `json:"rubric"`
}

type EvaluationRun struct {
	SchemaVersion   int     `json:"schema_version"`
	EvaluationRunID string  `json:"evaluation_run_id"`
	LabID           string  `json:"lab_id"`
	RubricID        string  `json:"rubric_id"`
	RubricVersion   int     `json:"rubric_version"`
	Trials          []Trial `json:"trials"`
}

type EvaluationRunRow struct {
	EvaluationRunID string        `json:"evaluation_run_id"`
	Run             EvaluationRun `json:"run"`
}

type ScoreRow struct {
	SchemaVersion        int      `json:"schema_version"`
	LabID                string   `json:"lab_id"`
	EvaluationRunID      string   `json:"evaluation_run_id"`
	TrialID              string   `json:"trial_id"`
	CaseID               string   `json:"case_id"`
	TrialNumber          int      `json:"trial_number"`
	CandidateSessionID   string   `json:"candidate_session_id"`
	JudgeRunExecutionID  string   `json:"judge_run_execution_id"`
	JudgeSessionID       string   `json:"judge_session_id"`
	RubricID             string   `json:"rubric_id"`
	RubricVersion        int      `json:"rubric_version"`
	CriterionID          string   `json:"criterion_id"`
	CriterionWeight      Number   `json:"criterion_weight"`
	CriterionResult      string   `json:"criterion_result"`
	CriterionPoints      Number   `json:"criterion_points"`
	CriterionHardGate    bool     `json:"criterion_hard_gate"`
	TrialHardFailed      bool     `json:"trial_hard_failed"`
	TrialScore           Number   `json:"trial_score"`
	Reason               string   `json:"reason"`
	EvidenceRefs         []string `json:"evidence_refs"`
	CandidateCompletedAt string   `json:"candidate_completed_at"`
	JudgedAt             string   `json:"judged_at"`
}

type TrialDetail struct {
	JudgeRunExecutionID          string             `json:"judge_run_execution_id"`
	DetailKey                    string             `json:"detail_key"`
	SchemaVersion                int                `json:"schema_version"`
	LabID                        string             `json:"lab_id"`
	EvaluationRunID              string             `json:"evaluation_run_id"`
	TrialID                      string             `json:"trial_id"`
	CandidateAgentLatencySeconds *Number            `json:"candidate_agent_latency_seconds"`
	JudgeAgentLatencySeconds     Number             `json:"judge_agent_latency_seconds"`
	ExpectedLabels               map[string]string  `json:"classification_expected_labels"`
	PredictedLabels              map[string]*string `json:"classification_predicted_labels"`
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
	Scores             []ScoreRow
	Details            []TrialDetail
	CandidateExecution Execution
	JudgeExecution     Execution
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

type Stats struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	Max  float64 `json:"max"`
}

type WorkflowTiming struct {
	Candidate float64 `json:"candidate"`
	Judge     float64 `json:"judge"`
}

type AgentTiming struct {
	Candidate *Stats `json:"candidate"`
	Judge     Stats  `json:"judge"`
}

type TimingSummary struct {
	WorkflowWallTimeSeconds WorkflowTiming `json:"workflow_wall_time_seconds"`
	AgentLatencySeconds     *AgentTiming   `json:"agent_latency_seconds"`
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

type ClassificationSummary struct {
	Criteria                  map[string]ClassificationMetrics `json:"criteria"`
	JointExactMatchAccuracy   *float64                         `json:"-"`
	jointExactMatchApplicable bool
}

func (s ClassificationSummary) MarshalJSON() ([]byte, error) {
	if !s.jointExactMatchApplicable {
		return json.Marshal(struct {
			Criteria map[string]ClassificationMetrics `json:"criteria"`
		}{Criteria: s.Criteria})
	}
	return json.Marshal(struct {
		Criteria                map[string]ClassificationMetrics `json:"criteria"`
		JointExactMatchAccuracy *float64                         `json:"joint_exact_match_accuracy"`
	}{Criteria: s.Criteria, JointExactMatchAccuracy: s.JointExactMatchAccuracy})
}

type Summary struct {
	TargetID           *string               `json:"target_id"`
	TargetIDs          []string              `json:"target_ids"`
	Tags               []string              `json:"tags"`
	SchemaVersion      int                   `json:"schema_version"`
	LabID              string                `json:"lab_id"`
	EvaluationRunID    string                `json:"evaluation_run_id"`
	Rubric             RubricSummary         `json:"rubric"`
	Trials             TrialSummary          `json:"trials"`
	CriterionPassRates map[string]*float64   `json:"criterion_pass_rates"`
	Timing             TimingSummary         `json:"timing"`
	Classification     ClassificationSummary `json:"classification"`
	Notices            []string              `json:"notices"`
}

type RubricSummary struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type labelPair struct {
	Expected  string
	Predicted *string
}
