# OfferLog — 本地 Docker 部署（唯一入口）
#
#   cp .env.example .env   # 必需！先建本地配置（默认值可直接用，按需修改）
#   make up                # 构建并启动 postgres + api + worker，并建好本地测试账号
#   make down              # 停止并移除所有容器数据（容器、网络、数据卷全部清空）
#
# 启动后访问 http://localhost:8080：make up 会自动建一个本地测试账号并把账号
# 密码打印在最后（见下方 SEED_* 变量），登录页「注册」也能自助开号
# （REGISTRATION_OPEN=true）；要闭门使用就在 .env 设 REGISTRATION_OPEN=false，
# 再用 api-admin create-user 建号（见 docs/runbook.md）。

# compose 默认只读 compose 文件旁的 deploy/.env，不读仓库根目录，故显式 --env-file。
# （up 已强制要求 .env 存在；down 在 .env 缺失时也允许执行，便于清理）
COMPOSE := docker compose -f deploy/compose.local.yaml $(if $(wildcard .env),--env-file .env)

# --- make up 自动创建的本地测试账号 -------------------------------------------
# 默认值可直接用；要改就写进 .env（SEED_USER_EMAIL / SEED_USER_PASSWORD /
# SEED_USER_NAME），也可以 `make up SEED_USER_EMAIL=...` 临时覆盖。
# SEED_USER=0 则不自动建号，改为在登录页「注册」开号。
#
# .env 只被 compose 的 --env-file 消费，make 自己不 include 它（注释、等号、
# 空格都会踩 make 语法），所以这四个键单独从文件里读出来。
seedkey = sed -n 's/^[[:space:]]*$(1)[[:space:]]*=//p' .env 2>/dev/null | tail -n 1 | sed 's/^[[:space:]]*//; s/^"//; s/"$$//; s/[[:space:]]*$$//'
seedval = $(if $(shell $(call seedkey,$(1))),$(shell $(call seedkey,$(1))),$(2))
SEED_ON       := $(call seedval,SEED_USER,1)
SEED_EMAIL    := $(call seedval,SEED_USER_EMAIL,demo@offerlog.local)
SEED_PASSWORD := $(call seedval,SEED_USER_PASSWORD,offerlog-demo-1234)
SEED_NAME     := $(call seedval,SEED_USER_NAME,Demo User)

.PHONY: help up down

help:
	@echo "offerlog:"
	@echo "  up      启动本地 Docker 栈，访问 http://localhost:8080（需先 cp .env.example .env）"
	@echo "          并自动建好测试账号 $(SEED_EMAIL) / $(SEED_PASSWORD)"
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
	@if [ "$(SEED_ON)" != "0" ]; then \
		$(COMPOSE) exec -T api /app/api-admin create-user \
			-email '$(SEED_EMAIL)' -password '$(SEED_PASSWORD)' -name '$(SEED_NAME)' \
			-if-not-exists || { \
			echo ""; \
			echo "建测试账号失败——栈本身已起来，可手动重跑："; \
			echo "  $(COMPOSE) exec api /app/api-admin create-user -email '$(SEED_EMAIL)' -password '$(SEED_PASSWORD)' -if-not-exists"; \
			exit 1; }; \
	fi
	@echo "OfferLog 已就绪：http://localhost:8080"
	@if [ "$(SEED_ON)" = "0" ]; then \
		echo "  未自动建号（SEED_USER=0）——在登录页「注册」标签开号"; \
	else \
		echo ""; \
		echo "  测试账号：$(SEED_EMAIL)  /  $(SEED_PASSWORD)"; \
		echo "  （首次 make up 建号；已存在则不改密码。改账号：编辑 .env 的 SEED_USER_*）"; \
	fi

down:
	$(COMPOSE) down -v --remove-orphans
	@echo "OfferLog 已停止：容器、网络与数据卷已全部移除。"
