# OfferLog — 本地 Docker 部署（唯一入口）
#
#   cp .env.example .env   # 必需！先建本地配置（默认值可直接用，按需修改）
#   make up                # 构建并启动 postgres + api + worker
#   make down              # 停止并移除所有容器数据（容器、网络、数据卷全部清空）
#
# 启动后访问 http://localhost:8080：本地栈默认开放注册（REGISTRATION_OPEN=true），
# 没有默认初始用户 —— 首次使用在登录页「注册」标签直接开号；要闭门使用就在 .env
# 设 REGISTRATION_OPEN=false，再用 api-admin create-user 建号（见 docs/runbook.md）。

# compose 默认只读 compose 文件旁的 deploy/.env，不读仓库根目录，故显式 --env-file。
# （up 已强制要求 .env 存在；down 在 .env 缺失时也允许执行，便于清理）
COMPOSE := docker compose -f deploy/compose.local.yaml $(if $(wildcard .env),--env-file .env)

.PHONY: help up down

help:
	@echo "offerlog:"
	@echo "  up      启动本地 Docker 栈，访问 http://localhost:8080（需先 cp .env.example .env）"
	@echo "  down    停止并删除所有容器数据（含数据卷，不可恢复）"

up:
	@test -f .env || { echo "缺少 .env：请先执行  cp .env.example .env  （可按需修改）再 make up"; exit 1; }
	@$(COMPOSE) up -d --build || { \
		echo ""; \
		echo "启动失败：若提示 postgres unhealthy，多半是 pgdata_local 数据卷当年是用别的"; \
		echo "POSTGRES_* 口令/用户名初始化的（口令只在首次建卷时生效，之后改 .env 不影响数据库）。"; \
		echo "处理：核对 .env 里的 POSTGRES_*，然后 make down 清掉旧卷再 make up（注意：会清空本地数据）。"; \
		exit 1; }
	@ok=""; for i in $$(seq 1 30); do curl -fsS -o /dev/null http://127.0.0.1:8080/health/ready && { ok=1; break; }; sleep 1; done; \
	if [ -z "$$ok" ]; then \
		echo "警告：http://localhost:8080/health/ready 30 秒内未就绪，查看日志："; \
		echo "  docker compose -f deploy/compose.local.yaml logs api"; \
		exit 1; \
	fi
	@echo "OfferLog: http://localhost:8080  (默认开放注册：在登录页「注册」开号)"

down:
	$(COMPOSE) down -v --remove-orphans
	@echo "OfferLog 已停止：容器、网络与数据卷已全部移除。"
