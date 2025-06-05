# GitHub Review Bot Makefile

.PHONY: help build test lint clean run docker-build docker-run deps coverage security

# Default target
help: ## Show this help message
	@echo "GitHub Review Bot - Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development targets
deps: ## Download dependencies
	go mod download
	go mod tidy

build: ## Build the application
	go build -o bin/review-bot .

test: ## Run tests
	go test -v -race ./...

test-coverage: ## Run tests with coverage
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

lint: ## Run linter
	golangci-lint run --timeout=5m

security: ## Run security scan
	gosec ./...

clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html

run: ## Run the application locally
	go run main.go

# Docker targets
docker-build: ## Build Docker image
	docker build -t github-review-bot:latest .

docker-run: ## Run Docker container
	docker run -p 8080:8080 --env-file .env github-review-bot:latest

docker-compose-up: ## Start all services with docker-compose
	docker-compose up -d

docker-compose-down: ## Stop all services
	docker-compose down

docker-compose-logs: ## View logs
	docker-compose logs -f

# CI/CD targets
ci-test: deps lint security test ## Run all CI tests

ci-build: ## Build for CI
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/review-bot .

# Monitoring targets
prometheus-config: ## Generate Prometheus config
	mkdir -p monitoring
	cat > monitoring/prometheus.yml << 'EOF'
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'review-bot'
    static_configs:
      - targets: ['review-bot:8080']
    metrics_path: '/metrics'
    scrape_interval: 5s

  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']
EOF

grafana-config: ## Generate Grafana config
	mkdir -p monitoring/grafana/dashboards monitoring/grafana/datasources
	cat > monitoring/grafana/datasources/prometheus.yml << 'EOF'
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
EOF

# Database targets (if using PostgreSQL)
db-migrate: ## Run database migrations
	@echo "Database migrations not implemented yet"

db-seed: ## Seed database with test data
	@echo "Database seeding not implemented yet"

# Deployment targets
deploy-staging: ## Deploy to staging
	@echo "Deploying to staging..."
	@echo "This would typically include:"
	@echo "- Building and pushing Docker image"
	@echo "- Updating Kubernetes/Cloud Run deployment"
	@echo "- Running smoke tests"

deploy-production: ## Deploy to production
	@echo "Deploying to production..."
	@echo "This would typically include:"
	@echo "- Checking CI/CD pipeline status"
	@echo "- Creating release tag"
	@echo "- Deploying to production environment"
	@echo "- Running health checks"

# Local development setup
setup: deps prometheus-config grafana-config ## Set up local development environment
	@echo "Setting up development environment..."
	@if [ ! -f .env ]; then cp .env.example .env; echo "Created .env file - please edit it with your configuration"; fi
	@echo "Run 'make docker-compose-up' to start all services"

# Performance testing
benchmark: ## Run benchmarks
	go test -bench=. -benchmem ./...

load-test: ## Run load tests (requires hey or similar tool)
	@if command -v hey >/dev/null 2>&1; then \
		echo "Running load test..."; \
		hey -n 1000 -c 10 http://localhost:8080/health; \
	else \
		echo "hey not installed. Install with: go install github.com/rakyll/hey@latest"; \
	fi

# Code quality
fmt: ## Format code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

mod-tidy: ## Tidy go modules
	go mod tidy

# Documentation
docs: ## Generate documentation
	@echo "Generating documentation..."
	godoc -http=:6060 &
	@echo "Documentation server started at http://localhost:6060"
	@echo "Press Ctrl+C to stop"

# Utility targets
check-env: ## Check environment variables
	@echo "Checking environment configuration..."
	@if [ -z "$$GITHUB_TOKEN" ]; then echo "❌ GITHUB_TOKEN not set"; else echo "✅ GITHUB_TOKEN is set"; fi