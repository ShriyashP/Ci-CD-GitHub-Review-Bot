package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/go-github/v57/github"
	"github.com/gorilla/mux"
)

func TestMain(m *testing.M) {
	// Set up test environment
	os.Setenv("GITHUB_TOKEN", "test-token")
	os.Setenv("WEBHOOK_SECRET", "test-secret")
	os.Setenv("PORT", "8080")
	
	code := m.Run()
	os.Exit(code)
}

func TestNewConfig(t *testing.T) {
	config := NewConfig()
	
	if config.GitHubToken != "test-token" {
		t.Errorf("Expected GitHubToken to be 'test-token', got %s", config.GitHubToken)
	}
	
	if config.WebhookSecret != "test-secret" {
		t.Errorf("Expected WebhookSecret to be 'test-secret', got %s", config.WebhookSecret)
	}
	
	if config.Port != "8080" {
		t.Errorf("Expected Port to be '8080', got %s", config.Port)
	}
	
	if config.MinReviewers != 2 {
		t.Errorf("Expected MinReviewers to be 2, got %d", config.MinReviewers)
	}
}

func TestNewReviewBot(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	if bot.client == nil {
		t.Error("Expected client to be initialized")
	}
	
	if bot.config.GitHubToken != config.GitHubToken {
		t.Error("Expected config to be set correctly")
	}
	
	if bot.stats == nil {
		t.Error("Expected stats to be initialized")
	}
}

func TestStatsCollector(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	// Test initial stats
	if bot.stats.TotalPRsProcessed != 0 {
		t.Errorf("Expected TotalPRsProcessed to be 0, got %d", bot.stats.TotalPRsProcessed)
	}
	
	if bot.stats.TotalChecksRun != 0 {
		t.Errorf("Expected TotalChecksRun to be 0, got %d", bot.stats.TotalChecksRun)
	}
	
	// Test stats collection
	bot.stats.mu.Lock()
	bot.stats.TotalPRsProcessed = 5
	bot.stats.TotalChecksRun = 20
	bot.stats.PRProcessingTimes["test-pr"] = 2 * time.Second
	bot.stats.CheckRunTimes["test"] = 100 * time.Millisecond
	bot.stats.mu.Unlock()
	
	avgTime := bot.calculateAverageProcessingTime()
	if avgTime != "2s" {
		t.Errorf("Expected average processing time to be '2s', got %s", avgTime)
	}
}

func TestRunSpecificCheck(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	ctx := context.Background()
	
	pr := &github.PullRequest{
		Number: github.Int(1),
	}
	
	tests := []struct {
		checkName string
		expected  string
	}{
		{"test", "test"},
		{"lint", "lint"},
		{"build", "build"},
		{"security", "security"},
		{"unknown", "unknown"},
	}
	
	for _, tt := range tests {
		t.Run(tt.checkName, func(t *testing.T) {
			result := bot.runSpecificCheck(ctx, "owner", "repo", pr, tt.checkName)
			if result.Name != tt.expected {
				t.Errorf("Expected check name to be %s, got %s", tt.expected, result.Name)
			}
		})
	}
}

func TestGenerateCommentBody(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	checks := []CheckResult{
		{Name: "test", Status: "success", Message: "All tests passed", Time: "100ms"},
		{Name: "lint", Status: "warning", Message: "Minor issues found", Time: "50ms"},
		{Name: "build", Status: "failure", Message: "Build failed", Time: "200ms"},
	}
	
	comment := bot.generateCommentBody(checks, true, "All policies satisfied")
	
	if !bytes.Contains([]byte(comment), []byte("Automated Review Results")) {
		t.Error("Expected comment to contain 'Automated Review Results'")
	}
	
	if !bytes.Contains([]byte(comment), []byte("✅ **test**")) {
		t.Error("Expected comment to contain success emoji for test")
	}
	
	if !bytes.Contains([]byte(comment), []byte("⚠️ **lint**")) {
		t.Error("Expected comment to contain warning emoji for lint")
	}
	
	if !bytes.Contains([]byte(comment), []byte("❌ **build**")) {
		t.Error("Expected comment to contain failure emoji for build")
	}
	
	if !bytes.Contains([]byte(comment), []byte("Ready to merge")) {
		t.Error("Expected comment to contain 'Ready to merge'")
	}
}

func TestHealthEndpoint(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	router := mux.NewRouter()
	router.HandleFunc("/health", bot.handleHealth).Methods("GET")
	
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, status)
	}
	
	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal("Failed to parse JSON response")
	}
	
	if response["status"] != "healthy" {
		t.Errorf("Expected status to be 'healthy', got %v", response["status"])
	}
	
	if response["version"] != "1.0.0" {
		t.Errorf("Expected version to be '1.0.0', got %v", response["version"])
	}
}

func TestStatsEndpoint(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	// Set up some test data
	bot.stats.mu.Lock()
	bot.stats.TotalPRsProcessed = 10
	bot.stats.TotalChecksRun = 40
	bot.stats.PRProcessingTimes["test-pr-1"] = 1 * time.Second
	bot.stats.PRProcessingTimes["test-pr-2"] = 3 * time.Second
	bot.stats.CheckRunTimes["test"] = 100 * time.Millisecond
	bot.stats.mu.Unlock()
	
	router := mux.NewRouter()
	router.HandleFunc("/stats", bot.handleStats).Methods("GET")
	
	req, err := http.NewRequest("GET", "/stats", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, status)
	}
	
	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal("Failed to parse JSON response")
	}
	
	if response["total_prs_processed"].(float64) != 10 {
		t.Errorf("Expected total_prs_processed to be 10, got %v", response["total_prs_processed"])
	}
	
	if response["total_checks_run"].(float64) != 40 {
		t.Errorf("Expected total_checks_run to be 40, got %v", response["total_checks_run"])
	}
	
	if response["avg_pr_processing_time"] != "2s" {
		t.Errorf("Expected avg_pr_processing_time to be '2s', got %v", response["avg_pr_processing_time"])
	}
}

func TestWebhookHandler(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	router := mux.NewRouter()
	router.HandleFunc("/webhook", bot.handleWebhook).Methods("POST")
	
	// Test with invalid payload
	req, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer([]byte("invalid json")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-GitHub-Event", "pull_request")
	
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Expected status code %d for invalid payload, got %d", http.StatusBadRequest, status)
	}
}

func TestCalculateAverageProcessingTime(t *testing.T) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	// Test with empty stats
	avgTime := bot.calculateAverageProcessingTime()
	if avgTime != "0s" {
		t.Errorf("Expected average time to be '0s' for empty stats, got %s", avgTime)
	}
	
	// Test with some data
	bot.stats.mu.Lock()
	bot.stats.PRProcessingTimes["pr1"] = 1 * time.Second
	bot.stats.PRProcessingTimes["pr2"] = 3 * time.Second
	bot.stats.PRProcessingTimes["pr3"] = 2 * time.Second
	bot.stats.mu.Unlock()
	
	avgTime = bot.calculateAverageProcessingTime()
	if avgTime != "2s" {
		t.Errorf("Expected average time to be '2s', got %s", avgTime)
	}
}

func BenchmarkRunSpecificCheck(b *testing.B) {
	config := NewConfig()
	bot := NewReviewBot(config)
	ctx := context.Background()
	
	pr := &github.PullRequest{
		Number: github.Int(1),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bot.runSpecificCheck(ctx, "owner", "repo", pr, "test")
	}
}

func BenchmarkGenerateCommentBody(b *testing.B) {
	config := NewConfig()
	bot := NewReviewBot(config)
	
	checks := []CheckResult{
		{Name: "test", Status: "success", Message: "All tests passed", Time: "100ms"},
		{Name: "lint", Status: "warning", Message: "Minor issues found", Time: "50ms"},
		{Name: "build", Status: "success", Message: "Build successful", Time: "200ms"},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bot.generateCommentBody(checks, true, "All policies satisfied")
	}
}