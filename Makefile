# OfferLog — 本地 Docker 部署（唯一入口）
#
#   make up     构建并启动 postgres + api + worker，自动创建首个账号
#   make down   停止并移除所有容器数据（容器、网络、数据卷全部清空）
#
# 启动后访问 http://localhost:8080，默认账号 me@example.com / testpass12345，
# 可用 LOCAL_ADMIN_EMAIL / LOCAL_ADMIN_PASSWORD 覆盖。

COMPOSE := docker compose -f deploy/compose.local.yaml

.PHONY: help up down

help:
	@echo "offerlog:"
	@echo "  up      启动本地 Docker 栈，访问 http://localhost:8080"
	@echo "  down    停止并删除所有容器数据（含数据卷，不可恢复）"

up:
	$(COMPOSE) up -d --build
	@echo "OfferLog: http://localhost:8080  (login: $${LOCAL_ADMIN_EMAIL:-me@example.com})"

down:
	$(COMPOSE) down -v --remove-orphans
	@echo "OfferLog 已停止：容器、网络与数据卷已全部移除。"