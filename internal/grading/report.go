package grading

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const epsilon = 1e-8

type Report struct {
	inputs                 Inputs
	run                    EvaluationRun
	rubric                 Rubric
	criteria               []Criterion
	classificationCriteria []Criterion
	scores                 []ScoreRow
	details                []TrialDetail
	legacy                 bool
	judgeExecutionID       string
	summary                Summary
}

func NewReport(inputs Inputs) (*Report, error) {
	r := &Report{inputs: inputs}
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := r.buildSummary(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Report) Summary() Summary { return r.summary }

func (r *Report) validate() error {
	var matches []EvaluationRun
	for _, row := range r.inputs.RunRows {
		if row.EvaluationRunID == r.inputs.RunID {
			matches = append(matches, row.Run)
		}
	}
	if len(matches) != 1 {
		return invalid("expected exactly one persisted Evaluation Run; found %d", len(matches))
	}
	r.run = matches[0]
	if r.run.SchemaVersion != 1 {
		return invalid("Evaluation Run has invalid schema version")
	}
	if r.run.EvaluationRunID != r.inputs.RunID {
		return invalid("Evaluation Run ID does not match %s", r.inputs.RunID)
	}
	if r.run.LabID != r.inputs.LabID {
		return invalid("Evaluation Run lab %q does not match %s", r.run.LabID, r.inputs.LabID)
	}
	if len(r.run.Trials) == 0 {
		return invalid("Evaluation Run has no trials")
	}
	trialIDs := make(map[string]bool, len(r.run.Trials))
	for _, trial := range r.run.Trials {
		if trial.TrialID == "" || trialIDs[trial.TrialID] {
			return invalid("Evaluation Run contains duplicate or empty trial IDs")
		}
		trialIDs[trial.TrialID] = true
	}

	r.rubric = r.run.Trials[0].Rubric
	r.criteria = r.rubric.Criteria
	if r.rubric.SchemaVersion != 1 {
		return invalid("Rubric has invalid schema version")
	}
	if len(r.criteria) == 0 {
		return invalid("Rubric has no criteria")
	}
	if err := r.validateRubric(); err != nil {
		return err
	}
	for _, trial := range r.run.Trials {
		if trial.Status != "completed" {
			return invalid("trial %s is not completed", trial.TrialID)
		}
		if trial.CaseID == "" || trial.TrialNumber < 1 || trial.CandidateSessionID == "" {
			return invalid("trial %s is structurally incomplete", trial.TrialID)
		}
		if _, err := time.Parse(time.RFC3339Nano, trial.CandidateCompletedAt); err != nil {
			return invalid("trial %s has invalid Candidate completion time", trial.TrialID)
		}
		if !reflect.DeepEqual(trial.Rubric, r.rubric) {
			return invalid("trial %s has a different rubric", trial.TrialID)
		}
	}
	if r.run.RubricID != r.rubric.RubricID {
		return invalid("Evaluation Run rubric ID does not match frozen rubric")
	}
	if r.run.RubricVersion != r.rubric.RubricVersion {
		return invalid("Evaluation Run rubric version does not match frozen rubric")
	}
	if _, err := validateExecution(r.inputs.CandidateExecution, r.inputs.RunID, "Candidate"); err != nil {
		return err
	}
	if err := r.validateScores(); err != nil {
		return err
	}
	if _, err := validateExecution(r.inputs.JudgeExecution, r.judgeExecutionID, "Judge"); err != nil {
		return err
	}
	return r.validateDetails()
}

func (r *Report) validateRubric() error {
	seen := make(map[string]bool, len(r.criteria))
	weightedTotal := 0.0
	for _, criterion := range r.criteria {
		if criterion.CriterionID == "" || seen[criterion.CriterionID] {
			return invalid("Rubric contains duplicate or empty criterion IDs")
		}
		seen[criterion.CriterionID] = true
		if !finite(float64(criterion.Weight)) || criterion.Weight < 0 {
			return invalid("criterion %s has invalid weight", criterion.CriterionID)
		}
		if criterion.HardGate {
			if !closeEnough(float64(criterion.Weight), 0) {
				return invalid("hard-gate criterion %s must have zero weight", criterion.CriterionID)
			}
		} else {
			weightedTotal += float64(criterion.Weight)
		}
		if criterion.Metric == nil {
			continue
		}
		if criterion.Metric.Type != "classification" {
			return invalid("criterion %s has unsupported metric metadata", criterion.CriterionID)
		}
		if len(criterion.Metric.Labels) < 2 || hasDuplicateOrEmpty(criterion.Metric.Labels) || slices.Contains(criterion.Metric.Labels, AbstentionLabel) {
			return invalid("classification criterion %s has invalid labels", criterion.CriterionID)
		}
		r.classificationCriteria = append(r.classificationCriteria, criterion)
	}
	if !closeEnough(weightedTotal, 100) {
		return invalid("non-gate rubric weights must total 100")
	}
	return nil
}

func (r *Report) validateScores() error {
	for _, score := range r.inputs.Scores {
		if score.EvaluationRunID == r.inputs.RunID {
			r.scores = append(r.scores, score)
		}
	}
	if len(r.scores) == 0 {
		return invalid("no Judge scores found; run: just judge %s RUN_ID=%s", r.inputs.LabID, r.inputs.RunID)
	}

	trials := make(map[string]Trial, len(r.run.Trials))
	for _, trial := range r.run.Trials {
		trials[trial.TrialID] = trial
	}
	criteria := make(map[string]Criterion, len(r.criteria))
	for _, criterion := range r.criteria {
		criteria[criterion.CriterionID] = criterion
	}
	seen := make(map[string]bool, len(r.scores))
	byTrial := make(map[string][]ScoreRow, len(trials))
	judgeIDs := make(map[string]bool)
	for _, score := range r.scores {
		key := score.TrialID + "\x00" + score.CriterionID
		if seen[key] {
			return invalid("Judge scores contain duplicate trial/criterion rows")
		}
		seen[key] = true
		trial, trialOK := trials[score.TrialID]
		criterion, criterionOK := criteria[score.CriterionID]
		if !trialOK || !criterionOK {
			return invalid("Judge score coverage contains unexpected rows")
		}
		if err := r.validateScoreIdentity(score, trial, criterion); err != nil {
			return err
		}
		byTrial[score.TrialID] = append(byTrial[score.TrialID], score)
		judgeIDs[score.JudgeRunExecutionID] = true
	}
	expectedRows := len(r.run.Trials) * len(r.criteria)
	if len(r.scores) != expectedRows {
		return invalid("Judge score coverage is incomplete (expected %d rows, found %d)", expectedRows, len(r.scores))
	}
	for trialID, rows := range byTrial {
		hardFailed := false
		points := 0.0
		for _, row := range rows {
			if row.CriterionHardGate && row.CriterionResult == "missed" {
				hardFailed = true
			}
			points += float64(row.CriterionPoints)
		}
		expectedScore := points
		if hardFailed {
			expectedScore = 0
		}
		for _, row := range rows {
			if row.TrialHardFailed != hardFailed {
				return invalid("trial %s hard-gate behavior is invalid", trialID)
			}
			if !closeEnough(float64(row.TrialScore), expectedScore) {
				return invalid("trial %s score arithmetic is invalid", trialID)
			}
		}
	}
	if len(byTrial) != len(trials) {
		return invalid("Judge score trial coverage is incomplete")
	}
	if len(judgeIDs) != 1 {
		return invalid("scores refer to multiple Judge executions")
	}
	for id := range judgeIDs {
		if id == "" {
			return invalid("score Judge execution ID is invalid")
		}
		r.judgeExecutionID = id
	}
	return nil
}

func (r *Report) validateScoreIdentity(score ScoreRow, trial Trial, criterion Criterion) error {
	prefix := fmt.Sprintf("score %s/%s", trial.TrialID, criterion.CriterionID)
	switch {
	case score.SchemaVersion != 1:
		return invalid("%s has invalid schema_version", prefix)
	case score.LabID != r.inputs.LabID:
		return invalid("%s has invalid lab_id", prefix)
	case score.TrialID != trial.TrialID:
		return invalid("%s has invalid trial_id", prefix)
	case score.CaseID != trial.CaseID:
		return invalid("%s has invalid case_id", prefix)
	case score.TrialNumber != trial.TrialNumber:
		return invalid("%s has invalid trial_number", prefix)
	case score.CandidateSessionID != trial.CandidateSessionID:
		return invalid("%s has invalid candidate_session_id", prefix)
	case score.RubricID != r.rubric.RubricID:
		return invalid("%s has invalid rubric_id", prefix)
	case score.RubricVersion != r.rubric.RubricVersion:
		return invalid("%s has invalid rubric_version", prefix)
	case score.CriterionHardGate != criterion.HardGate:
		return invalid("%s has invalid criterion_hard_gate", prefix)
	}
	if score.CriterionResult != "met" && score.CriterionResult != "missed" {
		return invalid("%s has invalid criterion result", prefix)
	}
	if !finite(float64(score.CriterionWeight)) || !closeEnough(float64(score.CriterionWeight), float64(criterion.Weight)) {
		return invalid("%s criterion weight does not match rubric", prefix)
	}
	expectedPoints := 0.0
	if score.CriterionResult == "met" {
		expectedPoints = float64(criterion.Weight)
	}
	if !finite(float64(score.CriterionPoints)) || !closeEnough(float64(score.CriterionPoints), expectedPoints) {
		return invalid("%s criterion points do not match result", prefix)
	}
	if !finite(float64(score.TrialScore)) {
		return invalid("%s trial score must be finite", prefix)
	}
	if score.JudgeSessionID == "" || score.EvidenceRefs == nil {
		return invalid("%s is structurally incomplete", prefix)
	}
	scoreCompletedAt, err := time.Parse(time.RFC3339Nano, score.CandidateCompletedAt)
	if err != nil {
		return invalid("%s Candidate completion time is invalid", prefix)
	}
	trialCompletedAt, err := time.Parse(time.RFC3339Nano, trial.CandidateCompletedAt)
	if err != nil || !scoreCompletedAt.Equal(trialCompletedAt) {
		return invalid("%s Candidate completion time does not match frozen Trial", prefix)
	}
	if _, err := time.Parse(time.RFC3339Nano, score.JudgedAt); err != nil {
		return invalid("%s judged time is invalid", prefix)
	}
	return nil
}

func (r *Report) validateDetails() error {
	for _, detail := range r.inputs.Details {
		if detail.EvaluationRunID == r.inputs.RunID {
			r.details = append(r.details, detail)
		}
	}
	if len(r.details) == 0 {
		r.legacy = true
		return nil
	}
	if len(r.details) != len(r.run.Trials) {
		return invalid("trial detail coverage is incomplete")
	}
	trials := make(map[string]Trial, len(r.run.Trials))
	for _, trial := range r.run.Trials {
		trials[trial.TrialID] = trial
	}
	classCriteria := make(map[string]Criterion, len(r.classificationCriteria))
	for _, criterion := range r.classificationCriteria {
		classCriteria[criterion.CriterionID] = criterion
	}
	seen := make(map[string]bool, len(r.details))
	for _, detail := range r.details {
		trial, ok := trials[detail.TrialID]
		if !ok || seen[detail.TrialID] {
			return invalid("trial details contain duplicate or unexpected trial rows")
		}
		seen[detail.TrialID] = true
		if detail.SchemaVersion != 1 || detail.LabID != r.inputs.LabID || detail.JudgeRunExecutionID != r.judgeExecutionID || detail.DetailKey != r.inputs.RunID+":"+trial.TrialID {
			return invalid("trial %s detail identity is invalid", trial.TrialID)
		}
		if (detail.CandidateAgentLatencySeconds != nil && (!finite(float64(*detail.CandidateAgentLatencySeconds)) || *detail.CandidateAgentLatencySeconds < 0)) ||
			!finite(float64(detail.JudgeAgentLatencySeconds)) || detail.JudgeAgentLatencySeconds < 0 {
			return invalid("trial %s agent latency is invalid", trial.TrialID)
		}
		if trial.CandidateAgentLatencySeconds != nil && (detail.CandidateAgentLatencySeconds == nil || !closeEnough(float64(*detail.CandidateAgentLatencySeconds), float64(*trial.CandidateAgentLatencySeconds))) {
			return invalid("trial %s detail Candidate latency does not match frozen Trial", trial.TrialID)
		}
		if len(detail.ExpectedLabels) != len(classCriteria) || len(detail.PredictedLabels) != len(classCriteria) {
			return invalid("trial %s detail classification keys do not match rubric", trial.TrialID)
		}
		for criterionID, criterion := range classCriteria {
			expected, expectedOK := detail.ExpectedLabels[criterionID]
			predicted, predictedOK := detail.PredictedLabels[criterionID]
			oracle, oracleOK := trial.Oracle.Criteria[criterionID]
			oracleExpected, oracleString := oracle.Expected.(string)
			if !expectedOK || !predictedOK || !oracleOK || !oracleString || expected != oracleExpected {
				return invalid("trial %s detail expected label does not match Oracle", trial.TrialID)
			}
			if !slices.Contains(criterion.Metric.Labels, expected) {
				return invalid("trial %s Oracle label is not declared by rubric", trial.TrialID)
			}
			if predicted != nil && !slices.Contains(criterion.Metric.Labels, *predicted) {
				return invalid("trial %s predicted label is not normalized", trial.TrialID)
			}
		}
	}
	return nil
}

func (r *Report) buildSummary() error {
	byTrial := make(map[string][]ScoreRow)
	for _, score := range r.scores {
		byTrial[score.TrialID] = append(byTrial[score.TrialID], score)
	}
	scores := make([]float64, 0, len(r.run.Trials))
	hardPassed := make(map[string]bool, len(r.run.Trials))
	distribution := make(map[float64]int)
	for _, trial := range r.run.Trials {
		score := float64(byTrial[trial.TrialID][0].TrialScore)
		scores = append(scores, score)
		distribution[score]++
		if !byTrial[trial.TrialID][0].TrialHardFailed {
			hardPassed[trial.TrialID] = true
		}
	}
	distributionScores := make([]float64, 0, len(distribution))
	for score := range distribution {
		distributionScores = append(distributionScores, score)
	}
	sort.Float64s(distributionScores)
	distributionSummary := make([]ScoreDistribution, 0, len(distributionScores))
	for _, score := range distributionScores {
		distributionSummary = append(distributionSummary, ScoreDistribution{Score: score, Count: distribution[score]})
	}
	candidateWall, err := validateExecution(r.inputs.CandidateExecution, r.inputs.RunID, "Candidate")
	if err != nil {
		return err
	}
	judgeWall, err := validateExecution(r.inputs.JudgeExecution, r.judgeExecutionID, "Judge")
	if err != nil {
		return err
	}
	passRates := make(map[string]*float64, len(r.criteria))
	for _, criterion := range r.criteria {
		met := 0
		total := 0
		for _, score := range r.scores {
			if score.CriterionID == criterion.CriterionID {
				total++
				if score.CriterionResult == "met" {
					met++
				}
			}
		}
		passRates[criterion.CriterionID] = ratio(met, total)
	}
	notices := []string{}
	if r.legacy {
		notices = append(notices, "Classification metrics and per-agent latency require an explicit re-judge for this historical run.")
	}
	tags := []string{}
	targetIDs := []string{}
	for _, trial := range r.run.Trials {
		if targetID := trial.Submission.Case.Payload.TargetID; targetID != "" {
			targetIDs = append(targetIDs, targetID)
		}
		for _, tag := range trial.Submission.Case.Tags {
			if tag.Name != "" {
				tags = append(tags, tag.Name)
			}
		}
	}
	slices.Sort(tags)
	tags = slices.Compact(tags)
	slices.Sort(targetIDs)
	targetIDs = slices.Compact(targetIDs)
	var targetID *string
	if len(targetIDs) == 1 {
		targetID = &targetIDs[0]
	}
	r.summary = Summary{
		TargetID:           targetID,
		TargetIDs:          targetIDs,
		Tags:               tags,
		SchemaVersion:      1,
		LabID:              r.inputs.LabID,
		EvaluationRunID:    r.inputs.RunID,
		Rubric:             RubricSummary{ID: r.rubric.RubricID, Version: r.rubric.RubricVersion},
		CriterionPassRates: passRates,
		Trials: TrialSummary{
			Count: len(scores), MeanScore: mean(scores), MedianScore: percentile(scores, 0.5),
			MinScore: slices.Min(scores), MaxScore: slices.Max(scores),
			HardGatePassRate: ratio(len(hardPassed), len(scores)), ScoreDistribution: distributionSummary,
		},
		Timing:         TimingSummary{WorkflowWallTimeSeconds: WorkflowTiming{Candidate: candidateWall, Judge: judgeWall}},
		Classification: r.classificationSummary(hardPassed),
		Notices:        notices,
	}
	if !r.legacy {
		candidate := make([]float64, 0, len(r.details))
		judge := make([]float64, 0, len(r.details))
		for _, detail := range r.details {
			if detail.CandidateAgentLatencySeconds != nil {
				candidate = append(candidate, float64(*detail.CandidateAgentLatencySeconds))
			}
			judge = append(judge, float64(detail.JudgeAgentLatencySeconds))
		}
		r.summary.Timing.AgentLatencySeconds = &AgentTiming{Judge: stats(judge)}
		if len(candidate) == len(r.details) {
			value := stats(candidate)
			r.summary.Timing.AgentLatencySeconds.Candidate = &value
		} else {
			r.summary.Notices = append(r.summary.Notices, "Candidate agent latency was not captured in this historical run; start a new Candidate Run to capture it.")
		}
	}
	return nil
}

func (r *Report) classificationSummary(hardPassed map[string]bool) ClassificationSummary {
	summary := ClassificationSummary{Criteria: map[string]ClassificationMetrics{}}
	if r.legacy {
		return summary
	}
	eligible := make([]TrialDetail, 0, len(r.details))
	for _, detail := range r.details {
		if hardPassed[detail.TrialID] {
			eligible = append(eligible, detail)
		}
	}
	for _, criterion := range r.classificationCriteria {
		pairs := make([]labelPair, 0, len(eligible))
		for _, detail := range eligible {
			pairs = append(pairs, labelPair{Expected: detail.ExpectedLabels[criterion.CriterionID], Predicted: detail.PredictedLabels[criterion.CriterionID]})
		}
		summary.Criteria[criterion.CriterionID] = classificationMetrics(criterion.Metric.Labels, pairs)
	}
	if len(r.classificationCriteria) > 1 {
		summary.jointExactMatchApplicable = true
		correct := 0
		for _, detail := range eligible {
			exact := true
			for _, criterion := range r.classificationCriteria {
				predicted := detail.PredictedLabels[criterion.CriterionID]
				if predicted == nil || *predicted != detail.ExpectedLabels[criterion.CriterionID] {
					exact = false
					break
				}
			}
			if exact {
				correct++
			}
		}
		summary.JointExactMatchAccuracy = ratio(correct, len(eligible))
	}
	return summary
}

func classificationMetrics(labels []string, pairs []labelPair) ClassificationMetrics {
	predictionLabels := append(slices.Clone(labels), AbstentionLabel)
	matrix := make(map[string]map[string]int, len(labels))
	for _, expected := range labels {
		matrix[expected] = make(map[string]int, len(predictionLabels))
		for _, predicted := range predictionLabels {
			matrix[expected][predicted] = 0
		}
	}
	for _, pair := range pairs {
		predicted := AbstentionLabel
		if pair.Predicted != nil {
			predicted = *pair.Predicted
		}
		matrix[pair.Expected][predicted]++
	}
	perClass := make(map[string]PerClassMetrics, len(labels))
	correct := 0
	var recalls []*float64
	var f1s []*float64
	weighted := 0.0
	for _, label := range labels {
		tp := matrix[label][label]
		correct += tp
		support := 0
		predictedCount := 0
		for _, count := range matrix[label] {
			support += count
		}
		for _, expected := range labels {
			predictedCount += matrix[expected][label]
		}
		precision := ratio(tp, predictedCount)
		recall := ratio(tp, support)
		f1 := ratio(2*tp, 2*tp+(predictedCount-tp)+(support-tp))
		perClass[label] = PerClassMetrics{Precision: precision, Recall: recall, F1: f1, Support: support}
		recalls = append(recalls, recall)
		f1s = append(f1s, f1)
		if f1 != nil {
			weighted += float64(support) * *f1
		}
	}
	metrics := ClassificationMetrics{
		Support: len(pairs), ConfusionMatrix: matrix, Accuracy: ratio(correct, len(pairs)),
		BalancedAccuracy: pointerMean(recalls), MacroF1: pointerMean(f1s),
		MCC: multiclassMCC(labels, pairs), PerClass: perClass,
	}
	if len(pairs) > 0 {
		value := weighted / float64(len(pairs))
		metrics.SupportWeightedF1 = &value
	}
	return metrics
}

func multiclassMCC(labels []string, pairs []labelPair) *float64 {
	if len(pairs) == 0 {
		return nil
	}
	allLabels := append(slices.Clone(labels), AbstentionLabel)
	matrix := make(map[string]map[string]int, len(allLabels))
	for _, expected := range allLabels {
		matrix[expected] = make(map[string]int, len(allLabels))
	}
	for _, pair := range pairs {
		predicted := AbstentionLabel
		if pair.Predicted != nil {
			predicted = *pair.Predicted
		}
		matrix[pair.Expected][predicted]++
	}
	total := float64(len(pairs))
	correct := 0.0
	expectedTotals := make(map[string]float64)
	predictedTotals := make(map[string]float64)
	for _, label := range allLabels {
		correct += float64(matrix[label][label])
		for _, predicted := range allLabels {
			expectedTotals[label] += float64(matrix[label][predicted])
			predictedTotals[predicted] += float64(matrix[label][predicted])
		}
	}
	covariance := correct * total
	expectedVariance := total * total
	predictedVariance := total * total
	for _, label := range allLabels {
		covariance -= expectedTotals[label] * predictedTotals[label]
		expectedVariance -= expectedTotals[label] * expectedTotals[label]
		predictedVariance -= predictedTotals[label] * predictedTotals[label]
	}
	denominator := math.Sqrt(expectedVariance * predictedVariance)
	if denominator == 0 {
		return nil
	}
	value := covariance / denominator
	return &value
}

func (r *Report) Write(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	sortedScores := slices.Clone(r.scores)
	sort.Slice(sortedScores, func(i, j int) bool {
		left, right := sortedScores[i], sortedScores[j]
		if left.CaseID != right.CaseID {
			return left.CaseID < right.CaseID
		}
		if left.TrialNumber != right.TrialNumber {
			return left.TrialNumber < right.TrialNumber
		}
		return left.CriterionID < right.CriterionID
	})
	if err := atomicWrite(filepath.Join(outputDir, "scores.csv"), func(file *os.File) error {
		writer := csv.NewWriter(file)
		if err := writer.Write(CSVColumns); err != nil {
			return err
		}
		for _, score := range sortedScores {
			evidence, err := json.Marshal(score.EvidenceRefs)
			if err != nil {
				return err
			}
			row := []string{
				strconv.Itoa(score.SchemaVersion), score.LabID, score.EvaluationRunID, score.TrialID, score.CaseID,
				strconv.Itoa(score.TrialNumber), score.CandidateSessionID, score.JudgeRunExecutionID, score.JudgeSessionID,
				score.RubricID, strconv.Itoa(score.RubricVersion), score.CriterionID, number(float64(score.CriterionWeight)),
				score.CriterionResult, number(float64(score.CriterionPoints)), strconv.FormatBool(score.CriterionHardGate),
				strconv.FormatBool(score.TrialHardFailed), number(float64(score.TrialScore)), score.Reason, string(evidence),
				score.CandidateCompletedAt, score.JudgedAt,
			}
			if err := writer.Write(row); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	}); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(outputDir, "summary.json"), func(file *os.File) error {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		return encoder.Encode(r.summary)
	})
}

func (r *Report) TerminalReport() string {
	var out strings.Builder
	fmt.Fprintf(&out, "Evaluation grade: %s / %s\n", r.inputs.LabID, r.inputs.RunID)
	fmt.Fprintf(&out, "Rubric: %s v%d\n", r.summary.Rubric.ID, r.summary.Rubric.Version)
	t := r.summary.Trials
	fmt.Fprintf(&out, "Trials: %d  Mean: %.2f  Median: %.2f  Min: %.2f  Max: %.2f\n", t.Count, t.MeanScore, t.MedianScore, t.MinScore, t.MaxScore)
	parts := make([]string, 0, len(t.ScoreDistribution))
	for _, item := range t.ScoreDistribution {
		parts = append(parts, fmt.Sprintf("%s=%d", number(item.Score), item.Count))
	}
	fmt.Fprintf(&out, "Score distribution: %s\n", strings.Join(parts, ", "))
	fmt.Fprintf(&out, "Hard-gate pass rate: %s\nCriterion pass rates:\n", percent(t.HardGatePassRate))
	for _, criterion := range r.criteria {
		fmt.Fprintf(&out, "  %s: %s\n", criterion.CriterionID, percent(r.summary.CriterionPassRates[criterion.CriterionID]))
	}
	fmt.Fprintf(&out, "Workflow wall time: Candidate %.2fs; Judge %.2fs\n", r.summary.Timing.WorkflowWallTimeSeconds.Candidate, r.summary.Timing.WorkflowWallTimeSeconds.Judge)
	if timing := r.summary.Timing.AgentLatencySeconds; timing != nil {
		if timing.Candidate != nil {
			fmt.Fprintf(&out, "Candidate agent latency: mean %.2fs  p50 %.2fs  p95 %.2fs  max %.2fs\n", timing.Candidate.Mean, timing.Candidate.P50, timing.Candidate.P95, timing.Candidate.Max)
		}
		fmt.Fprintf(&out, "Judge agent latency: mean %.2fs  p50 %.2fs  p95 %.2fs  max %.2fs\n", timing.Judge.Mean, timing.Judge.P50, timing.Judge.P95, timing.Judge.Max)
	}
	if len(r.summary.Classification.Criteria) > 0 {
		out.WriteString("Classification metrics (hard-gate-passed trials):\n")
		for _, criterion := range r.classificationCriteria {
			metrics := r.summary.Classification.Criteria[criterion.CriterionID]
			fmt.Fprintf(&out, "  %s: accuracy %s; balanced accuracy %s; macro-F1 %s; weighted-F1 %s; MCC %s\n",
				criterion.CriterionID, percent(metrics.Accuracy), percent(metrics.BalancedAccuracy), decimal(metrics.MacroF1), decimal(metrics.SupportWeightedF1), decimal(metrics.MCC))
			matrix, _ := json.Marshal(metrics.ConfusionMatrix)
			fmt.Fprintf(&out, "    confusion matrix: %s\n", matrix)
			for _, label := range criterion.Metric.Labels {
				values := metrics.PerClass[label]
				fmt.Fprintf(&out, "    %s: precision %s; recall %s; F1 %s; support %d\n", label, decimal(values.Precision), decimal(values.Recall), decimal(values.F1), values.Support)
			}
		}
		if r.summary.Classification.jointExactMatchApplicable {
			fmt.Fprintf(&out, "  Joint exact-match accuracy: %s\n", percent(r.summary.Classification.JointExactMatchAccuracy))
		}
	}
	for _, notice := range r.summary.Notices {
		fmt.Fprintf(&out, "Notice: %s\n", notice)
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func validateExecution(execution Execution, expectedID, role string) (float64, error) {
	if execution.ID == "" {
		return 0, invalid("%s execution could not be loaded", role)
	}
	if execution.ID != expectedID {
		return 0, invalid("%s execution ID does not match", role)
	}
	if execution.Status != "COMPLETED" {
		status := execution.Status
		if status == "" {
			status = "unknown"
		}
		if status == "RUNNING" || status == "PENDING" {
			return 0, invalid("%s execution is %s; wait for it to complete", role, status)
		}
		return 0, invalid("%s execution is %s; inspect the failed execution and start a new %s Run", role, status, role)
	}
	duration, err := execution.duration()
	if err != nil {
		return 0, invalid("%s execution has invalid workflow timestamps", role)
	}
	if duration < 0 {
		return 0, invalid("%s workflow has negative wall time", role)
	}
	return duration, nil
}

func atomicWrite(path string, write func(*os.File) error) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".grade-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := write(file); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func stats(values []float64) Stats {
	return Stats{Mean: mean(values), P50: percentile(values, 0.5), P95: percentile(values, 0.95), Max: slices.Max(values)}
}

func percentile(values []float64, quantile float64) float64 {
	sorted := slices.Clone(values)
	sort.Float64s(sorted)
	rank := float64(len(sorted)-1) * quantile
	lower := sorted[int(math.Floor(rank))]
	upper := sorted[int(math.Ceil(rank))]
	return lower + (upper-lower)*(rank-math.Floor(rank))
}

func mean(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func pointerMean(values []*float64) *float64 {
	numbers := make([]float64, 0, len(values))
	for _, value := range values {
		if value == nil {
			return nil
		}
		numbers = append(numbers, *value)
	}
	if len(numbers) == 0 {
		return nil
	}
	value := mean(numbers)
	return &value
}

func ratio(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
}

func hasDuplicateOrEmpty(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func closeEnough(left, right float64) bool { return math.Abs(left-right) <= epsilon }

func number(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func percent(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f%%", *value*100)
}

func decimal(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.4f", *value)
}
