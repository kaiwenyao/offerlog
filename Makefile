# OfferLog — 本地 Docker 部署（唯一入口）
#
#   cp .env.example .env   # 必需！先建本地配置（默认值可直接用，按需修改）
#   make up                # 构建并启动 postgres + api + worker，自动创建首个账号
#   make down              # 停止并移除所有容器数据（容器、网络、数据卷全部清空）
#
# 启动后访问 http://localhost:8080，默认账号 me@example.com / testpass12345，
# 在 .env 里覆盖（LOCAL_ADMIN_EMAIL / LOCAL_ADMIN_PASSWORD 等）。

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
	$(COMPOSE) up -d --build
	@echo "OfferLog: http://localhost:8080  (账号与口令见 .env；默认 me@example.com / testpass12345)"

down:
	$(COMPOSE) down -v --remove-orphans
	@echo "OfferLog 已停止：容器、网络与数据卷已全部移除。"