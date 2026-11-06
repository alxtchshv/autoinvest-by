# Project: AutoInvest BY FinTech Platform

## Architecture
AutoInvest BY is a production-grade automated micro-investing and Dollar-Cost Averaging (DCA) platform operating under the jurisdiction of the Republic of Belarus (Presidential Decree No. 8 "On the Development of the Digital Economy", High-Tech Park / HTP regime, partnerships with Whitebird and bePaid).

### High-Level Components
1. **Market Data & Risk Engine (`internal/market`, `internal/risk`)**:
   - Multi-asset polling workers: CoinGecko (crypto), Finnhub (US equities & ETFs), NBRB API (BYN currency rates).
   - Thread-safe in-memory cache with TTL and stale data fallback.
   - Risk Assessment Engine: Historical volatility ($\sigma$), Maximum Drawdown (MDD), Sharpe ratio calculation, 5-level Traffic Light scoring (1-Green to 5-Red), 7-question investor questionnaire scorer.
2. **Core Backend & Storage (`internal/database`, `internal/models`, `internal/dca`, `internal/cards`)**:
   - GORM dual-database support (embedded SQLite `autoinvest.db` with atomic transactions, PostgreSQL production profile).
   - Core domain models: Users, Sessions, KYC, Plans, Allocations, Assets, Quotes, Candles, Transactions, Cards, Snapshots, DCA Execution Logs.
   - Smart DCA Engine: Ticker-driven background scheduler, atomic execution with row-level locks, Buy the Dip trigger (-5% / -10% threshold with 24h cooldown), slippage guard (<1%).
   - Card Simulation & Payments: Luhn algorithm validator, Belkart (BIN 9112) routing, tokenization, 3D-Secure SMS/OTP challenge, monthly limits.
   - REST API: 33 endpoints across Auth, Profile, KYC, Risk, Assets, Market Data, Plans, Smart DCA, Cards, Analytics, and Health probes (`/healthz`, `/readyz`).
3. **Web Frontend (`web/`)**:
   - Embedded single-binary architecture via Go `embed.FS` (or standalone Next.js/React dashboard).
   - 12 comprehensive screens & modals: Dashboard, Plans Wizard, Asset Catalog, Asset Intelligence Card with TradingView Lightweight Charts (candlestick, PnL area, DCA markers), Time Machine Calculator, Bank Cards & Limits, History & Receipts, Pro Tier, Referral, Profile/Security, Risk Quiz, Deposit/Withdrawal.
   - Dark FinTech Design System: Deep Obsidian (`#0B0E14`), Slate 900 (`#0F172A`), Emerald (`#10B981`), Cyber Blue (`#3B82F6`), Alert Red (`#EF4444`).
   - Real-time updates via SSE / WebSockets.
4. **Telegram Bot Service (`internal/telegram`)**:
   - Built on `telebot.v3`.
   - Secure HMAC deep linking (`/start bind_<token>`).
   - Interactive commands: `/portfolio`, `/plans`, `/alerts`, `/help`.
   - Push notifications: automated DCA execution receipts, daily PnL summaries, instant "Buy the Dip" alerts with 1-click execution.
   - Token-bucket rate limiter (25 msgs/sec).
5. **Infrastructure & Deployment (`deploy/`, root Docker files)**:
   - Multi-stage Dockerfile (`golang:alpine` builder -> minimal runtime image <25 MB).
   - `docker-compose.yml` orchestrating Application, persistent data volumes, and Caddy 2 reverse proxy with automated Let's Encrypt TLS.
   - Systemd unit configuration for Linux VPS deployment.
6. **E2E Testing Track (`test/e2e/`)**:
   - Opaque-box requirement-driven testing harness covering Tiers 1-4.
   - Publication of `TEST_READY.md` upon completion.
   - Tier 5 adversarial chaos and concurrency verification.

---

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Multi-Asset Data Models | 15 GORM relational models (Users, KYC, Plans, Assets, Quotes, Candles, Cards, Transactions, etc.) | M2 | spec_miner_backend_1 |
| 2 | Dual DB Storage & Write Safety | SQLite + PostgreSQL support, atomic write-rename, automated backup snapshots | M2 | spec_miner_backend_1, explorer_infra_test_1 |
| 3 | Market Data Feeds & Cache | CoinGecko, Finnhub, NBRB API integrators with in-memory TTL cache and fallback | M1 | spec_miner_backend_1 |
| 4 | Risk Assessment Engine | Volatility, MDD, Sharpe calculation, 5-level Traffic Light gauge, 7-question Risk Quiz | M1 | spec_miner_backend_1 |
| 5 | Smart DCA Engine | Cron ticker scheduler, multi-asset allocation executor, Buy the Dip trigger, slippage check | M2 | spec_miner_backend_1 |
| 6 | Card & Payment Simulation | Luhn validator, Belkart BIN routing, tokenization, 3D-Secure OTP challenge, limits | M2 | spec_miner_backend_1 |
| 7 | Full REST API Contract | 33 endpoints covering Auth, Profile, KYC, Risk, Market, Plans, DCA, Cards, Analytics | M2 | spec_miner_backend_1 |
| 8 | Single-Binary Embed Packaging | Go `embed.FS` packaging for templates & static assets, zero-dependency deployment | M3 | explorer_infra_test_1 |
| 9 | 12 Comprehensive UI Views/Modals | Dashboard, Plans, Multi-Asset Catalog, Asset Card, Calculator, Cards, History, Pro, etc. | M3 | spec_miner_frontend_1 |
| 10 | TradingView Lightweight Charts | Interactive candlestick, PnL area curve, volume histogram, DCA purchase markers | M3 | spec_miner_frontend_1 |
| 11 | Real-time Streaming Tickers | SSE / WebSocket real-time quotes updates for crypto, US stocks, and BYN exchange rates | M3 | spec_miner_frontend_1 |
| 12 | Dark FinTech Design System | Obsidian/Emerald/Blue palette, responsive layout, accessible forms & interactive validation | M3 | spec_miner_frontend_1 |
| 13 | Interactive Wizards & Strict 100% Rule | 6-step plan constructor, strict 100% allocation sum check, +375 phone mask, card formatting | M3 | spec_miner_frontend_1 |
| 14 | Telegram Bot Service | telebot.v3 bot, HMAC deep linking, commands, DCA alerts, Buy the Dip push triggers | M4 | explorer_infra_test_1 |
| 15 | VPS Docker & Caddy TLS Infra | Multi-stage Dockerfile, docker-compose.yml, Caddy 2 auto-TLS, systemd unit, health checks | M4 | explorer_infra_test_1 |
| 16 | Autonomous Self-Loop & Quota Control | Task queue, checkpoint state machine, 5-hour quota tracking and scheduled hibernation | M4 | explorer_infra_test_1 |
| 17 | E2E Testing Suite (Tiers 1-4) | Opaque-box requirement-driven testing across feature, boundary, pairwise, and scenarios | E2E Track | explorer_infra_test_1 |
| 18 | Final E2E Pass & Adversarial Hardening | 100% E2E test pass execution followed by Tier 5 white-box chaos and concurrency hardening | M5 | orchestrator |

---

## Milestones

### Track A: Implementation Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Market Data & Risk Engine | Multi-asset pollers (CoinGecko, Finnhub, NBRB), in-memory cache, volatility/MDD/Sharpe risk calculator, 7-question quiz evaluator | none | DONE |
| M2 | Go Backend Core & Smart DCA | GORM models, dual SQLite/Postgres DB, 33 REST endpoints, Smart DCA engine with Buy the Dip, Belkart/Card simulation with 3DS | M1 | DONE |
| M3 | Full-Stack Frontend & TradingView | 12 comprehensive screens & modals, TradingView charts, real-time tickers (SSE/WS), plan wizard with 100% sum rule, embed.FS | M2 | DONE |
| M4 | Telegram Bot & VPS Docker Infra | telebot.v3 bot with HMAC deep linking & DCA push alerts, multi-stage Dockerfile, docker-compose with Caddy TLS, healthchecks | M2 | DONE |
| M5 | Final Milestone: 100% E2E & Adversarial | Phase 1: 101/101 E2E tests PASS. Phase 2: Tier 5 adversarial concurrency & chaos hardening PASSED. Victory Audit CONFIRMED. | M3, M4, E2E Track | DONE |

### Track B: E2E Testing Track
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| E2E | Requirement-Driven Test Suite | Design test infra (`test/e2e/`), test runner, Tier 1-4 test cases (>=5 per feature, boundary, pairwise, realistic scenarios), publish TEST_READY.md | none | DONE |

---

## Interface Contracts

### Market Data Engine ↔ Core Backend (`internal/market` ↔ `internal/dca`, `internal/handlers`)
- `market.GetQuote(ctx context.Context, symbol string) (models.Quote, error)`
- `market.GetCandles(ctx context.Context, symbol string, resolution string, from, to int64) ([]models.Candle, error)`
- `market.GetRates(ctx context.Context) (map[string]float64, error)`
- In-memory cache returns `stale` quote with warning if remote API fails or rate-limits.

### Risk Engine ↔ Plans & Constructor (`internal/risk` ↔ `internal/handlers`, `internal/models`)
- `risk.CalculateAssetRisk(candles []models.Candle) models.AssetRiskMetrics` (Annualized Volatility, MDD, Sharpe)
- `risk.CalculatePortfolioRisk(allocations []models.PlanAllocation, assetMetrics map[string]models.AssetRiskMetrics) models.PortfolioRiskMetrics`
- `risk.EvaluateQuiz(answers []int) (riskLevel int, profileName string, maxRecommendedRisk int)`
- Guardrail: if `portfolioRisk > userRiskProfile`, reject or require explicit user disclaimer acknowledgment.

### Smart DCA Engine ↔ Payment Gateway (`internal/dca` ↔ `internal/cards`)
- `cards.ChargeCard(ctx context.Context, cardID string, amountBYN float64, idempotencyKey string) (models.Transaction, error)`
- `dca.ExecutePlan(ctx context.Context, planID string, triggerType string) (models.DcaExecutionResult, error)`
- Idempotency key pattern: `dca_<plan_id>_<timestamp_date>`.
- Row-level lock on user balance / card limit prevents double charges.

### Core Backend ↔ Telegram Bot (`internal/handlers`, `internal/dca` ↔ `internal/telegram`)
- `telegram.SendDcaNotification(userID string, execResult models.DcaExecutionResult) error`
- `telegram.SendDipAlert(userID string, symbol string, dipPercent float64, currentPrice float64) error`
- Deep linking: Web generates `t.me/autoinvest_by_bot?start=bind_<jwt_token>`, bot handles `/start` and stores `chat_id` in user record.

---

## Code Layout
```
d:\Уник\AutoInvest_BY_Project\
├── cmd/
│   ├── server/
│   │   └── main.go                  # Main entry point (HTTP server + DCA daemon + Bot)
│   └── bot/
│       └── main.go                  # Standalone bot entry point (optional)
├── internal/
│   ├── models/                      # Domain entities & GORM schemas
│   │   └── models.go
│   ├── database/                    # Dual DB layer (SQLite/PostgreSQL) & migrations
│   │   ├── store.go
│   │   └── migrations.go
│   ├── market/                      # Market Data Engine (CoinGecko, Finnhub, NBRB, Cache)
│   │   ├── engine.go
│   │   ├── coingecko.go
│   │   ├── finnhub.go
│   │   └── nbrb.go
│   ├── risk/                        # Risk Assessment Engine (volatility, MDD, Sharpe, Quiz)
│   │   ├── engine.go
│   │   └── quiz.go
│   ├── dca/                         # Smart DCA scheduler & Buy the Dip trigger
│   │   ├── worker.go
│   │   └── trigger.go
│   ├── cards/                       # Belkart & card simulation, Luhn check, 3DS
│   │   └── simulator.go
│   ├── telegram/                    # Telegram Bot service (telebot.v3)
│   │   ├── bot.go
│   │   └── handlers.go
│   └── handlers/                    # REST API handlers & router
│       ├── router.go
│       ├── auth.go
│       ├── plans.go
│       ├── market.go
│       ├── cards.go
│       └── analytics.go
├── web/                             # Frontend assets
│   ├── embed.go                     # Go 1.16+ embed.FS declaration
│   ├── templates/                   # HTML templates (SSR / UI views)
│   │   ├── layout.html
│   │   ├── dashboard.html
│   │   ├── plans.html
│   │   ├── catalog.html
│   │   ├── calculator.html
│   │   ├── cards.html
│   │   ├── history.html
│   │   ├── pro.html
│   │   ├── referral.html
│   │   ├── settings.html
│   │   └── risk_quiz.html
│   └── static/                      # CSS, JS, TradingView canvas widgets
│       ├── css/
│       └── js/
│           └── tradingview-widget.js
├── deploy/                          # Production VPS & Containerization
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── Caddyfile
│   └── autoinvest.service
├── test/                            # E2E & Integration Tests
│   └── e2e/
│       ├── test_runner.py
│       ├── tier1_feature_test.py
│       ├── tier2_boundary_test.py
│       ├── tier3_pairwise_test.py
│       └── tier4_scenario_test.py
├── go.mod
├── go.sum
└── README.md
```
