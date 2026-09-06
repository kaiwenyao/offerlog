# OfferLog — 本地 Docker 部署（唯一入口）
#
#   make up     构建并启动 postgres + api + worker，自动创建首个账号
#   make down   停止并移除所有容器数据（容器、网络、数据卷全部清空）
#
# 启动后访问 http://localhost:8080，默认账号 me@example.com / testpass12345，
# 可用 LOCAL_ADMIN_EMAIL / LOCAL_ADMIN_PASSWORD 覆盖。
# 可选：cp .env.example .env 覆盖默认值（不建则全用默认）。

# 根目录存在 .env 时自动加载（--env-file 对缺失文件会报错，故仅存在时传）
COMPOSE := docker compose -f deploy/compose.local.yaml $(if $(wildcard .env),--env-file .env)

.PHONY: help up down

help:
	@echo "offerlog:"
	@echo "  up      启动本地 Docker 栈，访问 http://localhost:8080"
	@echo "  down    停止并删除所有容器数据（含数据卷，不可恢复）"

up:
	$(COMPOSE) up -d --build
	@echo "OfferLog: http://localhost:8080  (默认账号 me@example.com / testpass12345，可用 .env 覆盖)"

down:
	$(COMPOSE) down -v --remove-orphans
	@echo "OfferLog 已停止：容器、网络与数据卷已全部移除。"