.PHONY: build build-runtime web web-prototype run db image toolbox test test-web

# 本机工具基础层须与此架构一致；不声明支持跨架构运行。
RUNTIME_ARCH ?= amd64

build:
	go build -o bin/wscyber ./cmd/wscyber

# 镜像单独使用 Linux 静态产物，不覆盖宿主开发/运行的二进制。
# 只使用已安装的 Go 工具链与模块缓存；缺失时失败，不自动下载。
build-runtime:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(RUNTIME_ARCH) GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOGC=$${GOGC:-30} go build -p 1 -trimpath -o bin/runtime/wscyber ./cmd/wscyber

# 正式前端使用 React；依赖须已安装，构建不隐式安装或回退到 Vue 原型。
web:
	npm --offline --prefix web run build:react

# 仅保留历史工程原型的显式构建入口，不参与正式运行镜像。
web-prototype:
	npm --offline --prefix web run build

run: build
	@set -a; . ./.env; set +a; exec ./bin/wscyber

db:
	docker compose up -d --wait postgres

# 仅产生一个自定义运行镜像：控制面与各场景工具共用，不包含 PostgreSQL。
# 历史 Dockerfile 路径保留；构建上下文由根目录 .dockerignore 严格收窄。
image: build-runtime web
	docker build --pull=false --network=none --platform linux/$(RUNTIME_ARCH) -f deploy/toolbox/Dockerfile -t wscyber/runtime:local .

# 兼容旧命令，不再额外生成 toolbox 标签或镜像。
toolbox: image

# PostgreSQL 专项测试另需 WSCYBER_STORE_TEST_DATABASE_URL，必须是独立可清空测试库。
# 此入口仅为无数据库单元测试，不表示已完成 PG、浏览器或真实研究验收。
test:
	GOMAXPROCS=$${GOMAXPROCS:-2} GOGC=$${GOGC:-30} go test -p 1 ./internal/config ./internal/httpapi ./internal/execution ./internal/model ./internal/modelconfig ./internal/modelprovider ./internal/modelprobe ./internal/research ./internal/policy

# 组件与正式页面测试后做类型检查；浏览器/后端联调须单独执行。
test-web:
	npm --offline --prefix web run test:components
	npm --offline --prefix web run test:react
	npm --offline --prefix web run typecheck:react
