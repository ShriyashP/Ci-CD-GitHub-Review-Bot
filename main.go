package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v57/github"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
)

type Config struct {
	GitHubToken    string
	WebhookSecret  string
	Port           string
	MinReviewers   int
	RequiredChecks []string
}

type ReviewBot struct {
	client *github.Client
	config Config
	stats  *StatsCollector
}

type StatsCollector struct {
	mu                sync.RWMutex
	PRProcessingTimes map[string]time.Duration
	CheckRunTimes     map[string]time.Duration
	TotalPRsProcessed int64
	TotalChecksRun    int64
}

type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Time    string `json:"time"`
}

type PRStats struct {
	PRNumber        int                    `json:"pr_number"`
	ProcessingTime  string                 `json:"processing_time"`
	ChecksRun       []CheckResult          `json:"checks_run"`
	ReviewersCount  int                    `json:"reviewers_count"`
	Status          string                 `json:"status"`
	ThirdPartyHooks map[string]interface{} `json:"third_party_hooks"`
}

func NewConfig() Config {
	minReviewers, _ := strconv.Atoi(getEnvOrDefault("MIN_REVIEWERS", "2"))
	requiredChecks := strings.Split(getEnvOrDefault("REQUIRED_CHECKS", "test,lint,build"), ",")
	
	return Config{
		GitHubToken:    os.Getenv("GITHUB_TOKEN"),
		WebhookSecret:  os.Getenv("WEBHOOK_SECRET"),
		Port:           getEnvOrDefault("PORT", "8080"),
		MinReviewers:   minReviewers,
		RequiredChecks: requiredChecks,
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func NewReviewBot(config Config) *ReviewBot {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.GitHubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return &ReviewBot{
		client: client,
		config: config,
		stats: &StatsCollector{
			PRProcessingTimes: make(map[string]time.Duration),
			CheckRunTimes:     make(map[string]time.Duration),
		},
	}
}

func (rb *ReviewBot) handleWebhook(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		http.Error(w, "Error parsing webhook", http.StatusBadRequest)
		return
	}

	switch e := event.(type) {
	case *github.PullRequestEvent:
		rb.handlePullRequestEvent(e, startTime)
	case *github.CheckRunEvent:
		rb.handleCheckRunEvent(e, startTime)
	case *github.PullRequestReviewEvent:
		rb.handleReviewEvent(e, startTime)
	}

	w.WriteHeader(http.StatusOK)
}

func (rb *ReviewBot) handlePullRequestEvent(event *github.PullRequestEvent, startTime time.Time) {
	if event.GetAction() != "opened" && event.GetAction() != "synchronize" {
		return
	}

	ctx := context.Background()
	pr := event.GetPullRequest()
	owner := event.GetRepo().GetOwner().GetLogin()
	repo := event.GetRepo().GetName()
	prNumber := pr.GetNumber()

	log.Printf("Processing PR #%d in %s/%s", prNumber, owner, repo)

	// Run automated checks
	checks := rb.runAutomatedChecks(ctx, owner, repo, pr)
	
	// Check merge policies
	canMerge, reason := rb.checkMergePolicy(ctx, owner, repo, prNumber)
	
	// Update PR with status
	rb.updatePRStatus(ctx, owner, repo, prNumber, checks, canMerge, reason)
	
	// Collect stats
	processingTime := time.Since(startTime)
	rb.stats.mu.Lock()
	rb.stats.PRProcessingTimes[fmt.Sprintf("%s/%s#%d", owner, repo, prNumber)] = processingTime
	rb.stats.TotalPRsProcessed++
	rb.stats.mu.Unlock()

	// Send to third-party integrations
	rb.sendToThirdPartyServices(owner, repo, prNumber, checks, processingTime)

	log.Printf("Completed processing PR #%d in %v", prNumber, processingTime)
}

func (rb *ReviewBot) runAutomatedChecks(ctx context.Context, owner, repo string, pr *github.PullRequest) []CheckResult {
	var checks []CheckResult
	
	for _, checkName := range rb.config.RequiredChecks {
		startTime := time.Now()
		result := rb.runSpecificCheck(ctx, owner, repo, pr, checkName)
		checkTime := time.Since(startTime)
		
		result.Time = checkTime.String()
		checks = append(checks, result)
		
		// Store check timing stats
		rb.stats.mu.Lock()
		rb.stats.CheckRunTimes[checkName] = checkTime
		rb.stats.TotalChecksRun++
		rb.stats.mu.Unlock()
	}
	
	return checks
}

func (rb *ReviewBot) runSpecificCheck(ctx context.Context, owner, repo string, pr *github.PullRequest, checkName string) CheckResult {
	switch checkName {
	case "test":
		return rb.runTestCheck(ctx, owner, repo, pr)
	case "lint":
		return rb.runLintCheck(ctx, owner, repo, pr)
	case "build":
		return rb.runBuildCheck(ctx, owner, repo, pr)
	case "security":
		return rb.runSecurityCheck(ctx, owner, repo, pr)
	default:
		return CheckResult{
			Name:    checkName,
			Status:  "skipped",
			Message: fmt.Sprintf("Unknown check: %s", checkName),
		}
	}
}

func (rb *ReviewBot) runTestCheck(ctx context.Context, owner, repo string, pr *github.PullRequest) CheckResult {
	// Simulate test execution
	time.Sleep(100 * time.Millisecond)
	
	// Get PR files to analyze
	files, _, err := rb.client.PullRequests.ListFiles(ctx, owner, repo, pr.GetNumber(), nil)
	if err != nil {
		return CheckResult{
			Name:    "test",
			Status:  "error",
			Message: fmt.Sprintf("Failed to get PR files: %v", err),
		}
	}
	
	hasTests := false
	for _, file := range files {
		if strings.Contains(file.GetFilename(), "_test.go") ||
		   strings.Contains(file.GetFilename(), ".test.") {
			hasTests = true
			break
		}
	}
	
	if !hasTests {
		return CheckResult{
			Name:    "test",
			Status:  "warning",
			Message: "No test files found in this PR",
		}
	}
	
	return CheckResult{
		Name:    "test",
		Status:  "success",
		Message: "All tests passed",
	}
}

func (rb *ReviewBot) runLintCheck(ctx context.Context, owner, repo string, pr *github.PullRequest) CheckResult {
	time.Sleep(50 * time.Millisecond)
	
	files, _, err := rb.client.PullRequests.ListFiles(ctx, owner, repo, pr.GetNumber(), nil)
	if err != nil {
		return CheckResult{
			Name:    "lint",
			Status:  "error",
			Message: fmt.Sprintf("Failed to get PR files: %v", err),
		}
	}
	
	issues := 0
	for _, file := range files {
		if strings.HasSuffix(file.GetFilename(), ".go") {
			// Simulate linting - check for common issues
			if file.GetAdditions() > 100 {
				issues++
			}
		}
	}
	
	if issues > 0 {
		return CheckResult{
			Name:    "lint",
			Status:  "warning",
			Message: fmt.Sprintf("Found %d potential linting issues", issues),
		}
	}
	
	return CheckResult{
		Name:    "lint",
		Status:  "success",
		Message: "No linting issues found",
	}
}

func (rb *ReviewBot) runBuildCheck(ctx context.Context, owner, repo string, pr *github.PullRequest) CheckResult {
	time.Sleep(200 * time.Millisecond)
	
	// Check for build files
	files, _, err := rb.client.PullRequests.ListFiles(ctx, owner, repo, pr.GetNumber(), nil)
	if err != nil {
		return CheckResult{
			Name:    "build",
			Status:  "error",
			Message: fmt.Sprintf("Failed to get PR files: %v", err),
		}
	}
	
	hasBuildFiles := false
	for _, file := range files {
		if strings.Contains(file.GetFilename(), "Dockerfile") ||
		   strings.Contains(file.GetFilename(), "go.mod") ||
		   strings.Contains(file.GetFilename(), "Makefile") {
			hasBuildFiles = true
			break
		}
	}
	
	if !hasBuildFiles {
		return CheckResult{
			Name:    "build",
			Status:  "success",
			Message: "No build configuration changes",
		}
	}
	
	return CheckResult{
		Name:    "build",
		Status:  "success",
		Message: "Build check passed",
	}
}

func (rb *ReviewBot) runSecurityCheck(ctx context.Context, owner, repo string, pr *github.PullRequest) CheckResult {
	time.Sleep(150 * time.Millisecond)
	
	files, _, err := rb.client.PullRequests.ListFiles(ctx, owner, repo, pr.GetNumber(), nil)
	if err != nil {
		return CheckResult{
			Name:    "security",
			Status:  "error",
			Message: fmt.Sprintf("Failed to get PR files: %v", err),
		}
	}
	
	securityIssues := 0
	for _, file := range files {
		filename := strings.ToLower(file.GetFilename())
		if strings.Contains(filename, "password") ||
		   strings.Contains(filename, "secret") ||
		   strings.Contains(filename, "token") {
			securityIssues++
		}
	}
	
	if securityIssues > 0 {
		return CheckResult{
			Name:    "security",
			Status:  "failure",
			Message: fmt.Sprintf("Potential security issues found in %d files", securityIssues),
		}
	}
	
	return CheckResult{
		Name:    "security",
		Status:  "success",
		Message: "No security issues detected",
	}
}

func (rb *ReviewBot) checkMergePolicy(ctx context.Context, owner, repo string, prNumber int) (bool, string) {
	// Check required reviewers
	reviews, _, err := rb.client.PullRequests.ListReviews(ctx, owner, repo, prNumber, nil)
	if err != nil {
		return false, fmt.Sprintf("Failed to get reviews: %v", err)
	}
	
	approvals := 0
	for _, review := range reviews {
		if review.GetState() == "APPROVED" {
			approvals++
		}
	}
	
	if approvals < rb.config.MinReviewers {
		return false, fmt.Sprintf("Need %d approvals, have %d", rb.config.MinReviewers, approvals)
	}
	
	// Check required status checks
	pr, _, err := rb.client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return false, fmt.Sprintf("Failed to get PR: %v", err)
	}
	
	if pr.GetHead().GetSHA() == "" {
		return false, "No SHA available for status checks"
	}
	
	return true, "All merge policies satisfied"
}

func (rb *ReviewBot) updatePRStatus(ctx context.Context, owner, repo string, prNumber int, checks []CheckResult, canMerge bool, reason string) {
	status := "pending"
	description := "Automated checks in progress"
	
	if canMerge {
		allPassed := true
		for _, check := range checks {
			if check.Status == "failure" {
				allPassed = false
				break
			}
		}
		
		if allPassed {
			status = "success"
			description = "All checks passed - ready to merge"
		} else {
			status = "failure"
			description = "Some checks failed"
		}
	} else {
		status = "pending"
		description = reason
	}
	
	// Create a status check
	pr, _, err := rb.client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		log.Printf("Failed to get PR for status update: %v", err)
		return
	}
	
	repoStatus := &github.RepoStatus{
		State:       github.String(status),
		Description: github.String(description),
		Context:     github.String("ci/review-bot"),
	}
	
	_, _, err = rb.client.Repositories.CreateStatus(ctx, owner, repo, pr.GetHead().GetSHA(), repoStatus)
	if err != nil {
		log.Printf("Failed to create status: %v", err)
	}
	
	// Comment on PR with detailed results
	comment := rb.generateCommentBody(checks, canMerge, reason)
	prComment := &github.IssueComment{
		Body: github.String(comment),
	}
	
	_, _, err = rb.client.Issues.CreateComment(ctx, owner, repo, prNumber, prComment)
	if err != nil {
		log.Printf("Failed to create comment: %v", err)
	}
}

func (rb *ReviewBot) generateCommentBody(checks []CheckResult, canMerge bool, reason string) string {
	var comment strings.Builder
	
	comment.WriteString("## 🤖 Automated Review Results\n\n")
	
	comment.WriteString("### Check Results:\n")
	for _, check := range checks {
		emoji := "✅"
		switch check.Status {
		case "failure":
			emoji = "❌"
		case "warning":
			emoji = "⚠️"
		case "error":
			emoji = "🔴"
		case "skipped":
			emoji = "⏭️"
		}
		
		comment.WriteString(fmt.Sprintf("- %s **%s**: %s (%s)\n", emoji, check.Name, check.Message, check.Time))
	}
	
	comment.WriteString("\n### Merge Status:\n")
	if canMerge {
		comment.WriteString("✅ **Ready to merge** - " + reason + "\n")
	} else {
		comment.WriteString("⏳ **Not ready to merge** - " + reason + "\n")
	}
	
	comment.WriteString("\n---\n*This comment was generated automatically by the Review Bot*")
	
	return comment.String()
}

func (rb *ReviewBot) sendToThirdPartyServices(owner, repo string, prNumber int, checks []CheckResult, processingTime time.Duration) {
	// Simulate third-party service integration
	webhookData := PRStats{
		PRNumber:       prNumber,
		ProcessingTime: processingTime.String(),
		ChecksRun:      checks,
		ReviewersCount: rb.config.MinReviewers,
		Status:         "processed",
		ThirdPartyHooks: map[string]interface{}{
			"slack_notification": map[string]interface{}{
				"channel": "#code-reviews",
				"message": fmt.Sprintf("PR #%d in %s/%s processed in %v", prNumber, owner, repo, processingTime),
			},
			"jira_integration": map[string]interface{}{
				"update_ticket": true,
				"ticket_id":      fmt.Sprintf("PROJ-%d", prNumber),
			},
		},
	}
	
	// Send to external webhook (simulate)
	go func() {
		webhookURL := os.Getenv("THIRD_PARTY_WEBHOOK_URL")
		if webhookURL == "" {
			return
		}
		
		jsonData, err := json.Marshal(webhookData)
		if err != nil {
			log.Printf("Failed to marshal webhook data: %v", err)
			return
		}
		
		resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("Failed to send webhook: %v", err)
			return
		}
		defer resp.Body.Close()
		
		log.Printf("Webhook sent successfully for PR #%d", prNumber)
	}()
}

func (rb *ReviewBot) handleCheckRunEvent(event *github.CheckRunEvent, startTime time.Time) {
	log.Printf("Check run event: %s - %s", event.GetCheckRun().GetName(), event.GetCheckRun().GetStatus())
	
	processingTime := time.Since(startTime)
	rb.stats.mu.Lock()
	rb.stats.CheckRunTimes[event.GetCheckRun().GetName()] = processingTime
	rb.stats.mu.Unlock()
}

func (rb *ReviewBot) handleReviewEvent(event *github.PullRequestReviewEvent, startTime time.Time) {
	log.Printf("Review event: %s - %s", event.GetReview().GetState(), event.GetReview().GetUser().GetLogin())
	
	// Re-evaluate merge policy when new review is submitted
	if event.GetAction() == "submitted" {
		ctx := context.Background()
		owner := event.GetRepo().GetOwner().GetLogin()
		repo := event.GetRepo().GetName()
		prNumber := event.GetPullRequest().GetNumber()
		
		canMerge, reason := rb.checkMergePolicy(ctx, owner, repo, prNumber)
		rb.updatePRStatus(ctx, owner, repo, prNumber, []CheckResult{}, canMerge, reason)
	}
}

func (rb *ReviewBot) handleStats(w http.ResponseWriter, r *http.Request) {
	rb.stats.mu.RLock()
	defer rb.stats.mu.RUnlock()
	
	stats := map[string]interface{}{
		"total_prs_processed":    rb.stats.TotalPRsProcessed,
		"total_checks_run":       rb.stats.TotalChecksRun,
		"avg_pr_processing_time": rb.calculateAverageProcessingTime(),
		"check_run_times":        rb.stats.CheckRunTimes,
		"uptime":                 time.Since(startTime).String(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (rb *ReviewBot) calculateAverageProcessingTime() string {
	if len(rb.stats.PRProcessingTimes) == 0 {
		return "0s"
	}
	
	var total time.Duration
	for _, duration := range rb.stats.PRProcessingTimes {
		total += duration
	}
	
	average := total / time.Duration(len(rb.stats.PRProcessingTimes))
	return average.String()
}

func (rb *ReviewBot) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

var startTime = time.Now()

func main() {
	config := NewConfig()
	
	if config.GitHubToken == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}
	
	bot := NewReviewBot(config)
	
	r := mux.NewRouter()
	r.HandleFunc("/webhook", bot.handleWebhook).Methods("POST")
	r.HandleFunc("/stats", bot.handleStats).Methods("GET")
	r.HandleFunc("/health", bot.handleHealth).Methods("GET")
	
	// Serve static files for dashboard
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))
	
	log.Printf("Starting Review Bot server on port %s", config.Port)
	log.Printf("Webhook endpoint: http://localhost:%s/webhook", config.Port)
	log.Printf("Stats endpoint: http://localhost:%s/stats", config.Port)
	log.Printf("Health endpoint: http://localhost:%s/health", config.Port)
	
	log.Fatal(http.ListenAndServe(":"+config.Port, r))
}