package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/TracecatHQ/labs/internal/grading"
)

func main() {
	os.Exit(run())
}

func run() int {
	root := flag.String("root", ".", "repository root")
	lab := flag.String("lab", "", "zero-padded lab ID")
	runID := flag.String("run-id", "", "Candidate workflow execution ID")
	flag.Parse()
	if *lab == "" || *runID == "" {
		fmt.Fprintln(os.Stderr, "grade: --lab and --run-id are required")
		return 2
	}
	if err := grading.ValidateIdentifiers(*lab, *runID); err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	outputs, err := grading.LoadTerraformOutputs(ctx, *root, *lab)
	if err != nil {
		return fail(err)
	}
	client, err := grading.NewAPIClient(os.Getenv("TRACECAT_API_URL"), os.Getenv("TRACECAT_API_KEY"), nil)
	if err != nil {
		return fail(err)
	}
	inputs, err := grading.CollectInputs(ctx, client, outputs, *lab, *runID)
	if err != nil {
		return fail(err)
	}
	report, err := grading.NewReport(inputs)
	if err != nil {
		return fail(err)
	}
	outputDir := filepath.Join(*root, *lab, "results", *runID)
	if err := report.Write(outputDir); err != nil {
		return fail(fmt.Errorf("write grade report: %w", err))
	}
	fmt.Println(report.TerminalReport())
	fmt.Printf("Wrote %s\n", filepath.Join(outputDir, "scores.csv"))
	fmt.Printf("Wrote %s\n", filepath.Join(outputDir, "summary.json"))
	return 0
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "grade: %s\n", err)
	var validationError *grading.ValidationError
	if errors.As(err, &validationError) {
		return 2
	}
	return 1
}
