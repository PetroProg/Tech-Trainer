# ⚡ Tech Trainer — Interactive AI Practice Platform for Engineers

A high-performance, lightweight personal web application and PWA (Progressive Web App) designed for software engineers and systems developers. It delivers interactive coding quizzes, bite-sized engineering questions, code analysis challenges, and in-depth AI explanations generated on-demand by a local neural network.

Engineered specifically for ultra-low resource server environments (e.g., Intel Celeron N3060, **< 15 MB RAM** consumption in production) paired with an on-demand GPU worker running **Ollama** (`qwen2.5:7b` on NVIDIA RTX 3060 via Tailscale).

---

## 🚀 Architecture & Tech Stack

* **Backend:** Go 1.23 (`chi` router, zero-CGO SQLite via `modernc.org/sqlite` in WAL mode, native goroutines).
* **AI Worker Engine:**
  * **Smart Heartbeat:** Periodic health check every 4 hours + on-demand manual trigger.
  * **Buffer Control:** Automatically triggers batch generation when unanswered questions in any category drop below **15**.
  * **Batch Inference:** Produces batches of 50 practical questions when the worker PC is online.
  * **Deduplication:** SHA-256 content hashing on normalized question text and code snippets prevents redundant questions.
  * **Difficulty Calibration:** 2/3 Junior, 1/3 Middle tier practical real-world problems.
  * **Multi-Answer Logic:** Questions feature **1 to 4 correct answers** (single or multiple choice) to test true comprehension.
* **Frontend:** Vue 3 (Vite, TypeScript, Tailwind CSS, PrismJS syntax highlighter, Service Worker PWA).
* **Features:**
  * Interactive code snippets with syntax highlighting and one-click copy.
  * Instant AI Explanations analyzing underlying mechanics and why wrong answers fail.
  * Category management UI with customizable AI prompt instructions.
  * Question removal / report action for quick database cleanup.
  * Progress dashboard: daily streak counter, accuracy percentage, and category breakdown.

---

## 📦 Directory Structure

```text
/home/petroprog/Projects/tech-trainer/
├── cmd/
│   └── server/
│       └── main.go          # Application entrypoint & SPA file server
├── internal/
│   ├── api/
│   │   └── handlers.go      # REST API route handlers
│   ├── config/
│   │   └── config.go        # Environment variable configuration
│   ├── db/
│   │   └── db.go            # SQLite initialization, schema, and queries
│   ├── models/
│   │   └── models.go        # Data structures and DTOs
│   ├── ollama/
│   │   └── client.go        # Ollama HTTP API client
│   └── worker/
│       └── worker.go        # Periodic & threshold-based generator engine
├── frontend/
│   ├── public/              # PWA manifest icons & favicon
│   ├── src/
│   │   ├── components/      # UI components (CodeBlock, etc.)
│   │   ├── api.ts           # Frontend API client
│   │   ├── App.vue          # Main quiz view and management modals
│   │   └── main.ts          # Vue application bootstrap
│   ├── index.html
│   ├── package.json
│   ├── tailwind.config.js
│   └── vite.config.ts
├── data/                    # Persistent SQLite database volume
├── Dockerfile               # Multi-stage build (Node -> Go -> Alpine)
├── docker-compose.yml
└── README.md
```

---

## 🛠️ Deployment & Quick Start

### 1. Clone the repository
```bash
git clone git@github.com:PetroProg/tech-trainer.git /home/petroprog/Projects/tech-trainer
cd /home/petroprog/Projects/tech-trainer
```

### 2. Configure environment
Create or review `.env`:
```env
PORT=8080
DB_PATH=/data/trainer.db
OLLAMA_URL=http://100.114.251.81:11434
OLLAMA_MODEL=qwen2.5:7b
CHECK_INTERVAL_HOURS=4
MIN_CATEGORY_BUFFER=15
BATCH_SIZE=50
```

### 3. Launch with Docker Compose
```bash
docker compose up -d --build
```

Access the application in your browser:
* Local network / Tailscale: `https://petroprog.tail703e6c.ts.net:8443` (or `http://100.107.4.120:8080`)
* Inspect logs: `docker logs -f tech-trainer`
* Resource stats: `docker stats tech-trainer`

---

## 🔒 License
Project done with AI. [LICENSE](./LICENSE)
