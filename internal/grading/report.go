package grading

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const epsilon = 1e-8

type Report struct {
	inputs   Inputs
	run      EvaluationRunRow
	profile  ScoringProfile
	trials   []TrialRow
	scores   []ScoreRow
	metrics  []MetricRow
	attempts []ScoringAttempt
	summary  Summary
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
	for _, row := range r.inputs.RunRows {
		if row.EvaluationRunID == r.inputs.RunID {
			if r.run.EvaluationRunID != "" {
				return invalid("expected exactly one Evaluation Run")
			}
			r.run = row
		}
	}
	if r.run.EvaluationRunID == "" {
		return invalid("expected exactly one Evaluation Run")
	}
	if r.run.SchemaVersion != 2 || r.run.LabID != r.inputs.LabID || r.run.Status != "completed" {
		return invalid("Evaluation Run identity or status is invalid")
	}
	if len(r.run.SelectedCaseIDs) != r.run.ExpectedTrialCount || r.run.TemplateDigest == "" || hasDuplicateOrEmpty(r.run.SelectedCaseIDs) {
		return invalid("Evaluation Run frozen selection is invalid")
	}
	r.profile = r.run.Profile
	if err := validateProfile(r.profile); err != nil {
		return err
	}
	if r.run.ProfileID != r.profile.ProfileID || r.run.ProfileVersion != r.profile.ProfileVersion {
		return invalid("Evaluation Run profile identity does not match frozen profile")
	}

	seenTrials := map[string]bool{}
	for _, row := range r.inputs.Trials {
		if row.EvaluationRunID != r.inputs.RunID {
			continue
		}
		if row.SchemaVersion != 2 || row.LabID != r.inputs.LabID || row.TrialID == "" || seenTrials[row.TrialID] {
			return invalid("Trials contain duplicate or invalid rows")
		}
		seenTrials[row.TrialID] = true
		if row.TrialKey != r.inputs.RunID+":"+row.TrialID || row.Status != "completed" || row.Trial.Status != "completed" {
			return invalid("trial %s is incomplete", row.TrialID)
		}
		if row.Trial.TrialID != row.TrialID || row.Trial.CaseID != row.CaseID || row.Trial.TrialNumber != row.TrialNumber {
			return invalid("trial %s identity is invalid", row.TrialID)
		}
		if row.Trial.CandidateSessionID == "" || row.Trial.FinalAnswer == nil {
			return invalid("trial %s has no frozen Candidate answer", row.TrialID)
		}
		if _, err := time.Parse(time.RFC3339Nano, row.Trial.CandidateCompletedAt); err != nil {
			return invalid("trial %s has invalid Candidate completion time", row.TrialID)
		}
		if !profilesEqual(row.Trial.Profile, r.profile) {
			return invalid("trial %s has a different frozen profile", row.TrialID)
		}
		r.trials = append(r.trials, row)
	}
	if len(r.trials) == 0 || len(r.trials) != r.run.CompletedTrialCount || r.run.CompletedTrialCount != r.run.ExpectedTrialCount {
		return invalid("Evaluation Run trial coverage is incomplete")
	}
	selected := slices.Clone(r.run.SelectedCaseIDs)
	actual := make([]string, 0, len(r.trials))
	for _, row := range r.trials {
		actual = append(actual, row.CaseID)
	}
	slices.Sort(selected)
	slices.Sort(actual)
	if !slices.Equal(selected, actual) {
		return invalid("Evaluation Run trials do not match the frozen Case selection")
	}
	if err := r.validateEvidence(); err != nil {
		return err
	}
	if r.inputs.CandidateExecution.ID != r.inputs.RunID {
		return invalid("Candidate execution ID does not match")
	}
	if err := r.validateScores(); err != nil {
		return err
	}
	executions := map[string]Execution{}
	for _, execution := range r.inputs.ScoringExecutions {
		executions[execution.ID] = execution
	}
	for _, score := range r.scores {
		execution, ok := executions[score.ScoringRunExecutionID]
		if !ok || execution.ID != score.ScoringRunExecutionID {
			return invalid("Scoring execution %s could not be loaded", score.ScoringRunExecutionID)
		}
	}
	return r.validateMetricsAndAttempts()
}

func (r *Report) validateEvidence() error {
	byTrial := map[string][]EvidenceRow{}
	validTrials := map[string]Trial{}
	for _, row := range r.trials {
		validTrials[row.TrialID] = row.Trial
	}
	for _, row := range r.inputs.Evidence {
		if row.EvaluationRunID != r.inputs.RunID {
			continue
		}
		if row.SchemaVersion != 2 || row.LabID != r.inputs.LabID || validTrials[row.TrialID].TrialID == "" || row.EvidenceKey != fmt.Sprintf("%s:%s:%d", r.inputs.RunID, row.TrialID, row.Sequence) {
			return invalid("frozen evidence contains an invalid row")
		}
		byTrial[row.TrialID] = append(byTrial[row.TrialID], row)
	}
	for trialID, trial := range validTrials {
		rows := byTrial[trialID]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Sequence < rows[j].Sequence })
		if len(rows) < 3 {
			return invalid("trial %s has incomplete frozen evidence", trialID)
		}
		for sequence, row := range rows {
			if row.Sequence != sequence {
				return invalid("trial %s evidence is not ordered", trialID)
			}
			if sequence < 3 && row.EvidenceType != []string{"final_answer", "case", "comments"}[sequence] {
				return invalid("trial %s is missing a required public evidence snapshot", trialID)
			}
			if sequence >= 3 && row.EvidenceType != "tool_call" && row.EvidenceType != "tool_result" {
				return invalid("trial %s contains unsupported evidence", trialID)
			}
		}
		answer, _ := json.Marshal(trial.FinalAnswer)
		frozen, _ := json.Marshal(rows[0].Evidence)
		if string(answer) != string(frozen) {
			return invalid("trial %s final answer does not match frozen evidence", trialID)
		}
	}
	return nil
}

func profilesEqual(a, b ScoringProfile) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func validateProfile(profile ScoringProfile) error {
	if profile.SchemaVersion != 2 || profile.ProfileID == "" || profile.ProfileVersion < 1 {
		return invalid("Scoring Profile identity is invalid")
	}
	if !slices.Contains([]string{"deterministic", "model", "hybrid"}, profile.Scorer.Kind) || profile.Scorer.WorkflowAlias == "" {
		return invalid("Scoring Profile scorer is invalid")
	}
	if profile.Scorer.Kind == "model" && profile.Scorer.JudgePreset == "" {
		return invalid("model scorer requires a judge preset")
	}
	seenCriteria := map[string]bool{}
	weightedTotal := 0.0
	for _, criterion := range profile.Criteria {
		if criterion.CriterionID == "" || seenCriteria[criterion.CriterionID] {
			return invalid("Scoring Profile contains duplicate or empty criterion IDs")
		}
		seenCriteria[criterion.CriterionID] = true
		if !slices.Contains([]string{"binary", "numeric"}, criterion.Type) || !finite(float64(criterion.Weight)) || criterion.Weight < 0 {
			return invalid("criterion %s is invalid", criterion.CriterionID)
		}
		min, max := criterionBounds(criterion)
		if !finite(min) || !finite(max) || max <= min || float64(criterion.PassThreshold) < min || float64(criterion.PassThreshold) > max {
			return invalid("criterion %s has invalid range or pass threshold", criterion.CriterionID)
		}
		if criterion.Type == "binary" && criterion.Range != nil {
			return invalid("binary criterion %s must not declare a range", criterion.CriterionID)
		}
		if criterion.Type == "numeric" && criterion.Range == nil {
			return invalid("numeric criterion %s requires a range", criterion.CriterionID)
		}
		if criterion.HardGate {
			if !closeEnough(float64(criterion.Weight), 0) {
				return invalid("hard-gate criterion %s must have zero weight", criterion.CriterionID)
			}
		} else {
			weightedTotal += float64(criterion.Weight)
		}
	}
	if len(profile.Criteria) == 0 || !closeEnough(weightedTotal, 100) {
		return invalid("non-gate profile weights must total 100")
	}
	seenMetrics := map[string]bool{}
	for _, metric := range profile.Metrics {
		if metric.MetricID == "" || seenMetrics[metric.MetricID] || !slices.Contains([]string{"classification", "numeric", "boolean", "text"}, metric.Type) {
			return invalid("Scoring Profile contains an invalid metric")
		}
		seenMetrics[metric.MetricID] = true
		if metric.SourceCriterionID != "" && !seenCriteria[metric.SourceCriterionID] {
			return invalid("metric %s refers to an unknown criterion", metric.MetricID)
		}
		if metric.Type == "classification" {
			if len(metric.Labels) < 2 || hasDuplicateOrEmpty(metric.Labels) || slices.Contains(metric.Labels, AbstentionLabel) {
				return invalid("classification metric %s has invalid labels", metric.MetricID)
			}
		} else if len(metric.Labels) != 0 {
			return invalid("metric %s has labels for a non-classification type", metric.MetricID)
		}
	}
	return nil
}

func criterionBounds(criterion Criterion) (float64, float64) {
	if criterion.Type == "binary" {
		return 0, 1
	}
	if criterion.Range == nil {
		return math.NaN(), math.NaN()
	}
	return float64(criterion.Range.Min), float64(criterion.Range.Max)
}

func (r *Report) validateScores() error {
	trials := map[string]Trial{}
	for _, row := range r.trials {
		trials[row.TrialID] = row.Trial
	}
	criteria := map[string]Criterion{}
	for _, criterion := range r.profile.Criteria {
		criteria[criterion.CriterionID] = criterion
	}
	seen := map[string]bool{}
	byTrial := map[string][]ScoreRow{}
	for _, score := range r.inputs.Scores {
		if score.EvaluationRunID != r.inputs.RunID {
			continue
		}
		key := score.TrialID + "\x00" + score.CriterionID
		if seen[key] {
			return invalid("scores contain duplicate trial/criterion rows")
		}
		seen[key] = true
		trial, trialOK := trials[score.TrialID]
		criterion, criterionOK := criteria[score.CriterionID]
		if !trialOK || !criterionOK {
			return invalid("score coverage contains unexpected rows")
		}
		if err := r.validateScore(score, trial, criterion); err != nil {
			return err
		}
		byTrial[score.TrialID] = append(byTrial[score.TrialID], score)
		r.scores = append(r.scores, score)
	}
	if len(r.scores) == 0 {
		return invalid("no scores found; run: just judge %s RUN_ID=%s", r.inputs.LabID, r.inputs.RunID)
	}
	if len(r.scores) != len(trials)*len(criteria) {
		return invalid("score coverage is incomplete")
	}
	for trialID, rows := range byTrial {
		hardFailed := false
		points := 0.0
		for _, row := range rows {
			if row.CriterionHardGate && !row.CriterionPassed {
				hardFailed = true
			}
			points += float64(row.CriterionPoints)
		}
		expectedTotal := points
		if hardFailed {
			expectedTotal = 0
		}
		for _, row := range rows {
			if row.TrialHardFailed != hardFailed || !closeEnough(float64(row.TrialScore), expectedTotal) {
				return invalid("trial %s score arithmetic or hard-gate behavior is invalid", trialID)
			}
		}
	}
	return nil
}

func (r *Report) validateScore(score ScoreRow, trial Trial, criterion Criterion) error {
	prefix := fmt.Sprintf("score %s/%s", trial.TrialID, criterion.CriterionID)
	if score.SchemaVersion != 2 || score.LabID != r.inputs.LabID || score.ScoreKey != r.inputs.RunID+":"+trial.TrialID+":"+criterion.CriterionID ||
		score.CaseID != trial.CaseID || score.TrialNumber != trial.TrialNumber || score.CandidateSessionID != trial.CandidateSessionID ||
		score.ProfileID != r.profile.ProfileID || score.ProfileVersion != r.profile.ProfileVersion || score.ScorerKind != r.profile.Scorer.Kind ||
		score.ScorerWorkflowAlias != r.profile.Scorer.WorkflowAlias || score.CriterionType != criterion.Type ||
		score.CriterionHardGate != criterion.HardGate || score.ScoringRunExecutionID == "" || score.ScoringAttemptID == "" {
		return invalid("%s identity is invalid", prefix)
	}
	if r.profile.Scorer.JudgePreset != "" && score.JudgeSessionID == "" {
		return invalid("%s is missing its judge session", prefix)
	}
	if !finite(float64(score.CriterionValue)) || !finite(float64(score.CriterionPoints)) || !finite(float64(score.TrialScore)) {
		return invalid("%s contains a non-finite number", prefix)
	}
	min, max := criterionBounds(criterion)
	value := float64(score.CriterionValue)
	if value < min-epsilon || value > max+epsilon {
		return invalid("%s value is outside the criterion range", prefix)
	}
	passed := value+epsilon >= float64(criterion.PassThreshold)
	points := float64(criterion.Weight) * (value - min) / (max - min)
	if score.CriterionPassed != passed || !closeEnough(float64(score.CriterionWeight), float64(criterion.Weight)) || !closeEnough(float64(score.CriterionPoints), points) {
		return invalid("%s normalization is invalid", prefix)
	}
	if score.Reason == "" || score.EvidenceRefs == nil {
		return invalid("%s is structurally incomplete", prefix)
	}
	completed, err := time.Parse(time.RFC3339Nano, score.CandidateCompletedAt)
	if err != nil {
		return invalid("%s has an invalid Candidate completion time", prefix)
	}
	frozen, _ := time.Parse(time.RFC3339Nano, trial.CandidateCompletedAt)
	if !completed.Equal(frozen) {
		return invalid("%s Candidate completion time does not match frozen Trial", prefix)
	}
	if _, err := time.Parse(time.RFC3339Nano, score.ScoredAt); err != nil {
		return invalid("%s has invalid scored_at", prefix)
	}
	return nil
}

func (r *Report) validateMetricsAndAttempts() error {
	definitions := map[string]MetricDefinition{}
	for _, definition := range r.profile.Metrics {
		definitions[definition.MetricID] = definition
	}
	trialIDs := map[string]bool{}
	trialExecution := map[string]string{}
	trialAttempt := map[string]string{}
	for _, row := range r.trials {
		trialIDs[row.TrialID] = true
	}
	for _, score := range r.scores {
		trialExecution[score.TrialID] = score.ScoringRunExecutionID
		trialAttempt[score.TrialID] = score.ScoringAttemptID
	}
	seenMetrics := map[string]bool{}
	for _, metric := range r.inputs.Metrics {
		if metric.EvaluationRunID != r.inputs.RunID {
			continue
		}
		definition, ok := definitions[metric.MetricID]
		key := metric.TrialID + "\x00" + metric.MetricID
		if !ok || !trialIDs[metric.TrialID] || seenMetrics[key] {
			return invalid("metrics contain duplicate or unexpected rows")
		}
		seenMetrics[key] = true
		if metric.SchemaVersion != 2 || metric.LabID != r.inputs.LabID || metric.MetricKey != r.inputs.RunID+":"+metric.TrialID+":"+metric.MetricID ||
			metric.ProfileID != r.profile.ProfileID || metric.ProfileVersion != r.profile.ProfileVersion || metric.MetricType != definition.Type ||
			metric.ScoringRunExecutionID != trialExecution[metric.TrialID] || metric.ScoringAttemptID != trialAttempt[metric.TrialID] || !json.Valid(metric.Value) || !json.Valid(metric.ExpectedValue) {
			return invalid("metric %s/%s identity or value is invalid", metric.TrialID, metric.MetricID)
		}
		if err := validateMetricValue(definition, metric.Value, false); err != nil {
			return invalid("metric %s/%s value is invalid: %v", metric.TrialID, metric.MetricID, err)
		}
		if err := validateMetricValue(definition, metric.ExpectedValue, true); err != nil {
			return invalid("metric %s/%s expected value is invalid: %v", metric.TrialID, metric.MetricID, err)
		}
		r.metrics = append(r.metrics, metric)
	}
	if len(r.metrics) != len(r.trials)*len(definitions) {
		return invalid("metric coverage is incomplete")
	}
	seenAttempts := map[string]bool{}
	for _, attempt := range r.inputs.Attempts {
		if attempt.EvaluationRunID != r.inputs.RunID {
			continue
		}
		if !trialIDs[attempt.TrialID] || seenAttempts[attempt.TrialID] || attempt.SchemaVersion != 2 || attempt.LabID != r.inputs.LabID ||
			attempt.AttemptKey != r.inputs.RunID+":"+attempt.TrialID || attempt.Status != "completed" ||
			attempt.ScoringRunExecutionID != trialExecution[attempt.TrialID] || attempt.ScoringAttemptID != trialAttempt[attempt.TrialID] || attempt.ScorerKind != r.profile.Scorer.Kind ||
			attempt.ScorerWorkflowAlias != r.profile.Scorer.WorkflowAlias || attempt.ScoringAttemptID == "" {
			return invalid("scoring attempt %s is invalid", attempt.TrialID)
		}
		seenAttempts[attempt.TrialID] = true
		r.attempts = append(r.attempts, attempt)
	}
	if len(r.attempts) != len(r.trials) {
		return invalid("scoring attempt coverage is incomplete")
	}
	return nil
}

func validateMetricValue(definition MetricDefinition, raw json.RawMessage, expected bool) error {
	if string(raw) == "null" {
		return nil // null is an abstention or an intentionally absent expectation
	}
	switch definition.Type {
	case "classification", "text":
		var value string
		if json.Unmarshal(raw, &value) != nil || (definition.Type == "classification" && !slices.Contains(definition.Labels, value)) {
			return fmt.Errorf("expected a declared string")
		}
	case "numeric":
		var value Number
		if json.Unmarshal(raw, &value) != nil || !finite(float64(value)) {
			return fmt.Errorf("expected a finite number")
		}
	case "boolean":
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("expected a boolean")
		}
	}
	return nil
}

func (r *Report) buildSummary() error {
	byTrial := map[string][]ScoreRow{}
	for _, score := range r.scores {
		byTrial[score.TrialID] = append(byTrial[score.TrialID], score)
	}
	var totals []float64
	distribution := map[float64]int{}
	hardPassed := map[string]bool{}
	for _, trial := range r.trials {
		rows := byTrial[trial.TrialID]
		total := float64(rows[0].TrialScore)
		totals = append(totals, total)
		distribution[total]++
		if !rows[0].TrialHardFailed {
			hardPassed[trial.TrialID] = true
		}
	}
	var keys []float64
	for value := range distribution {
		keys = append(keys, value)
	}
	sort.Float64s(keys)
	var dist []ScoreDistribution
	for _, value := range keys {
		dist = append(dist, ScoreDistribution{Score: value, Count: distribution[value]})
	}
	criteria := map[string]CriterionAggregate{}
	for _, criterion := range r.profile.Criteria {
		var values, points []float64
		passed := 0
		for _, score := range r.scores {
			if score.CriterionID == criterion.CriterionID {
				values = append(values, float64(score.CriterionValue))
				points = append(points, float64(score.CriterionPoints))
				if score.CriterionPassed {
					passed++
				}
			}
		}
		criteria[criterion.CriterionID] = CriterionAggregate{MeanValue: mean(values), MeanPoints: mean(points), PassRate: ratio(passed, len(values))}
	}
	candidateWall := 0.0
	if r.inputs.CandidateExecution.Status == "COMPLETED" {
		candidateWall, _ = validateExecution(r.inputs.CandidateExecution, r.inputs.RunID, "Candidate")
	}
	scoringWall := 0.0
	executionIDs := make([]string, 0, len(r.inputs.ScoringExecutions))
	for _, execution := range r.inputs.ScoringExecutions {
		if execution.Status == "COMPLETED" {
			wall, _ := validateExecution(execution, execution.ID, "Scoring")
			scoringWall += wall
		}
		executionIDs = append(executionIDs, execution.ID)
	}
	slices.Sort(executionIDs)
	trialStatuses := map[string]int{}
	for _, row := range r.trials {
		trialStatuses[row.Status]++
	}
	attemptStatuses := map[string]int{}
	for _, row := range r.attempts {
		attemptStatuses[row.Status]++
	}
	tags, targets := []string{}, []string{}
	for _, row := range r.trials {
		if id := row.Trial.Submission.Case.Payload.TargetID; id != "" {
			targets = append(targets, id)
		}
		for _, tag := range row.Trial.Submission.Case.Tags {
			if tag.Name != "" {
				tags = append(tags, tag.Name)
			}
		}
	}
	slices.Sort(tags)
	tags = slices.Compact(tags)
	slices.Sort(targets)
	targets = slices.Compact(targets)
	var target *string
	if len(targets) == 1 {
		target = &targets[0]
	}
	r.summary = Summary{
		SchemaVersion: 2, LabID: r.inputs.LabID, EvaluationRunID: r.inputs.RunID,
		TargetID: target, TargetIDs: targets, Tags: tags,
		Profile:    ProfileSummary{ID: r.profile.ProfileID, Version: r.profile.ProfileVersion},
		Status:     StatusSummary{Run: r.run.Status, Trials: trialStatuses, ScoringAttempts: attemptStatuses},
		Provenance: ProvenanceSummary{ScorerKind: r.profile.Scorer.Kind, ScorerWorkflowAlias: r.profile.Scorer.WorkflowAlias, ScoringExecutionIDs: executionIDs},
		Trials:     TrialSummary{Count: len(totals), MeanScore: mean(totals), MedianScore: percentile(totals, .5), MinScore: slices.Min(totals), MaxScore: slices.Max(totals), HardGatePassRate: ratio(len(hardPassed), len(totals)), ScoreDistribution: dist},
		Criteria:   criteria, Metrics: r.metricAggregates(hardPassed), WorkflowTiming: WorkflowTiming{Candidate: candidateWall, Scoring: scoringWall},
	}
	return nil
}

func (r *Report) metricAggregates(hardPassed map[string]bool) map[string]MetricAggregate {
	result := map[string]MetricAggregate{}
	for _, definition := range r.profile.Metrics {
		rows := []MetricRow{}
		for _, row := range r.metrics {
			if row.MetricID == definition.MetricID && hardPassed[row.TrialID] {
				rows = append(rows, row)
			}
		}
		aggregate := MetricAggregate{Type: definition.Type, Count: len(rows)}
		switch definition.Type {
		case "classification":
			pairs := make([]labelPair, 0, len(rows))
			for _, row := range rows {
				if string(row.ExpectedValue) == "null" {
					continue
				}
				var expected string
				_ = json.Unmarshal(row.ExpectedValue, &expected)
				var predicted *string
				if string(row.Value) != "null" {
					var value string
					_ = json.Unmarshal(row.Value, &value)
					predicted = &value
				}
				pairs = append(pairs, labelPair{Expected: expected, Predicted: predicted})
			}
			value := classificationMetrics(definition.Labels, pairs)
			aggregate.Classification = &value
		case "numeric":
			values := []float64{}
			for _, row := range rows {
				if string(row.Value) == "null" {
					continue
				}
				var value Number
				_ = json.Unmarshal(row.Value, &value)
				values = append(values, float64(value))
			}
			if len(values) > 0 {
				value := mean(values)
				aggregate.Mean = &value
			}
			aggregate.StandardError = standardError(values)
		}
		result[definition.MetricID] = aggregate
	}
	return result
}

func classificationMetrics(labels []string, pairs []labelPair) ClassificationMetrics {
	predictionLabels := append(slices.Clone(labels), AbstentionLabel)
	matrix := map[string]map[string]int{}
	for _, expected := range labels {
		matrix[expected] = map[string]int{}
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
	perClass := map[string]PerClassMetrics{}
	correct, weighted := 0, 0.0
	var recalls, f1s []*float64
	for _, label := range labels {
		tp, support, predictedCount := matrix[label][label], 0, 0
		correct += tp
		for _, count := range matrix[label] {
			support += count
		}
		for _, expected := range labels {
			predictedCount += matrix[expected][label]
		}
		precision, recall := ratio(tp, predictedCount), ratio(tp, support)
		f1 := ratio(2*tp, 2*tp+(predictedCount-tp)+(support-tp))
		perClass[label] = PerClassMetrics{Precision: precision, Recall: recall, F1: f1, Support: support}
		recalls, f1s = append(recalls, recall), append(f1s, f1)
		if f1 != nil {
			weighted += float64(support) * *f1
		}
	}
	result := ClassificationMetrics{Support: len(pairs), ConfusionMatrix: matrix, Accuracy: ratio(correct, len(pairs)), BalancedAccuracy: pointerMean(recalls), MacroF1: pointerMean(f1s), MCC: multiclassMCC(labels, pairs), PerClass: perClass}
	if len(pairs) > 0 {
		value := weighted / float64(len(pairs))
		result.SupportWeightedF1 = &value
	}
	return result
}

func multiclassMCC(labels []string, pairs []labelPair) *float64 {
	if len(pairs) == 0 {
		return nil
	}
	all := append(slices.Clone(labels), AbstentionLabel)
	matrix := map[string]map[string]int{}
	for _, label := range all {
		matrix[label] = map[string]int{}
	}
	for _, pair := range pairs {
		predicted := AbstentionLabel
		if pair.Predicted != nil {
			predicted = *pair.Predicted
		}
		matrix[pair.Expected][predicted]++
	}
	total, correct := float64(len(pairs)), 0.0
	expectedTotals, predictedTotals := map[string]float64{}, map[string]float64{}
	for _, label := range all {
		correct += float64(matrix[label][label])
		for _, predicted := range all {
			expectedTotals[label] += float64(matrix[label][predicted])
			predictedTotals[predicted] += float64(matrix[label][predicted])
		}
	}
	covariance, expectedVariance, predictedVariance := correct*total, total*total, total*total
	for _, label := range all {
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
	sort.Slice(r.scores, func(i, j int) bool { return r.scores[i].ScoreKey < r.scores[j].ScoreKey })
	if err := atomicWrite(filepath.Join(outputDir, "scores.csv"), func(file *os.File) error {
		writer := csv.NewWriter(file)
		_ = writer.Write(ScoreCSVColumns)
		for _, score := range r.scores {
			evidence, _ := json.Marshal(score.EvidenceRefs)
			_ = writer.Write([]string{strconv.Itoa(score.SchemaVersion), score.LabID, score.EvaluationRunID, score.TrialID, score.CaseID, strconv.Itoa(score.TrialNumber), score.CandidateSessionID, score.ScoringRunExecutionID, score.ScoringAttemptID, score.JudgeSessionID, score.ProfileID, strconv.Itoa(score.ProfileVersion), score.ScorerKind, score.ScorerWorkflowAlias, score.CriterionID, score.CriterionType, number(float64(score.CriterionWeight)), number(float64(score.CriterionValue)), number(float64(score.CriterionPoints)), strconv.FormatBool(score.CriterionHardGate), strconv.FormatBool(score.CriterionPassed), strconv.FormatBool(score.TrialHardFailed), number(float64(score.TrialScore)), score.Reason, string(evidence), score.CandidateCompletedAt, score.ScoredAt})
		}
		writer.Flush()
		return writer.Error()
	}); err != nil {
		return err
	}
	sort.Slice(r.metrics, func(i, j int) bool { return r.metrics[i].MetricKey < r.metrics[j].MetricKey })
	if err := atomicWrite(filepath.Join(outputDir, "metrics.csv"), func(file *os.File) error {
		writer := csv.NewWriter(file)
		_ = writer.Write(MetricCSVColumns)
		for _, metric := range r.metrics {
			_ = writer.Write([]string{strconv.Itoa(metric.SchemaVersion), metric.LabID, metric.EvaluationRunID, metric.TrialID, metric.CaseID, strconv.Itoa(metric.TrialNumber), metric.ScoringRunExecutionID, metric.ScoringAttemptID, metric.ProfileID, strconv.Itoa(metric.ProfileVersion), metric.MetricID, metric.MetricType, string(metric.Value), string(metric.ExpectedValue), metric.ScoredAt})
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
	fmt.Fprintf(&out, "Profile: %s v%d; scorer: %s via %s\n", r.profile.ProfileID, r.profile.ProfileVersion, r.profile.Scorer.Kind, r.profile.Scorer.WorkflowAlias)
	t := r.summary.Trials
	fmt.Fprintf(&out, "Trials: %d  Mean: %.2f  Median: %.2f  Min: %.2f  Max: %.2f\n", t.Count, t.MeanScore, t.MedianScore, t.MinScore, t.MaxScore)
	fmt.Fprintf(&out, "Hard-gate pass rate: %s\n", percent(t.HardGatePassRate))
	for _, criterion := range r.profile.Criteria {
		value := r.summary.Criteria[criterion.CriterionID]
		fmt.Fprintf(&out, "  %s: mean value %s; mean points %s; pass rate %s\n", criterion.CriterionID, number(value.MeanValue), number(value.MeanPoints), percent(value.PassRate))
	}
	for _, metric := range r.profile.Metrics {
		value := r.summary.Metrics[metric.MetricID]
		if value.Classification != nil {
			fmt.Fprintf(&out, "  metric %s: accuracy %s; macro-F1 %s\n", metric.MetricID, percent(value.Classification.Accuracy), decimal(value.Classification.MacroF1))
		} else if value.Mean != nil {
			fmt.Fprintf(&out, "  metric %s: mean %s\n", metric.MetricID, number(*value.Mean))
		} else {
			fmt.Fprintf(&out, "  metric %s: %d values\n", metric.MetricID, value.Count)
		}
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func validateExecution(execution Execution, expectedID, role string) (float64, error) {
	if execution.ID == "" || execution.ID != expectedID {
		return 0, invalid("%s execution ID does not match", role)
	}
	if execution.Status != "COMPLETED" {
		if execution.Status == "RUNNING" || execution.Status == "PENDING" {
			return 0, invalid("%s execution is %s; wait for it to complete", role, execution.Status)
		}
		return 0, invalid("%s execution is %s", role, execution.Status)
	}
	duration, err := execution.duration()
	if err != nil || duration < 0 {
		return 0, invalid("%s execution has invalid workflow timestamps", role)
	}
	return duration, nil
}

func atomicWrite(path string, write func(*os.File) error) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".grade-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err := write(file); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func percentile(values []float64, quantile float64) float64 {
	sorted := slices.Clone(values)
	sort.Float64s(sorted)
	rank := float64(len(sorted)-1) * quantile
	lower, upper := sorted[int(math.Floor(rank))], sorted[int(math.Ceil(rank))]
	return lower + (upper-lower)*(rank-math.Floor(rank))
}

func mean(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func standardError(values []float64) *float64 {
	if len(values) < 2 {
		return nil
	}
	average := mean(values)
	squared := 0.0
	for _, value := range values {
		delta := value - average
		squared += delta * delta
	}
	sampleVariance := squared / float64(len(values)-1)
	result := math.Sqrt(sampleVariance / float64(len(values)))
	return &result
}

func pointerMean(values []*float64) *float64 {
	numbers := []float64{}
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

func finite(value float64) bool            { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func closeEnough(left, right float64) bool { return math.Abs(left-right) <= epsilon }

func hasDuplicateOrEmpty(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

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
