SHELL := /usr/bin/env bash

.DEFAULT_GOAL := help

GO ?= go
NPM ?= npm
NODE ?= node
APP_NAME ?= chatgpt2codex
CMD_PATH ?= ./cmd/$(APP_NAME)
DIST_DIR ?= dist
NPM_PUBLISH_ARGS ?= --access public
ARGS ?=

LOCAL_BIN := bin/$(APP_NAME)
ifeq ($(OS),Windows_NT)
LOCAL_BIN := bin/$(APP_NAME).exe
endif

.PHONY: help fmt test build run install release package prepublish publish-dry-run publish clean verify

help: ## 显示可用命令
	@awk 'BEGIN {FS = ":.*## "; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## 格式化 Go 代码
	$(GO) fmt ./...

test: ## 运行 Go 测试
	$(GO) test ./...

build: ## 构建本地 CLI 到 $(LOCAL_BIN)
	@mkdir -p "$(dir $(LOCAL_BIN))"
	CGO_ENABLED=0 $(GO) build -trimpath -o "$(LOCAL_BIN)" $(CMD_PATH)

run: ## 运行 CLI，示例：make run ARGS='tools'
	$(GO) run $(CMD_PATH) $(ARGS)

install: ## 安装 CLI 到 GOBIN 或 GOPATH/bin
	CGO_ENABLED=0 $(GO) install $(CMD_PATH)

release: ## 交叉编译发布二进制到 $(DIST_DIR)
	bash scripts/build-release.sh "$(DIST_DIR)"

package: ## 校验 npm 包内容（npm pack --dry-run）
	$(NPM) pack --dry-run

prepublish: ## 执行 npm 发布前校验
	$(NODE) scripts/prepublish.js

publish-dry-run: prepublish ## 演练 npm 发布流程
	$(NPM) publish --dry-run $(NPM_PUBLISH_ARGS)

publish: prepublish ## 发布 npm 包
	$(NPM) publish $(NPM_PUBLISH_ARGS)

clean: ## 清理本地构建产物
	rm -f bin/$(APP_NAME) bin/$(APP_NAME).exe
	rm -rf "$(DIST_DIR)"
	rm -f $(APP_NAME)-*.tgz

verify: fmt test build package ## 运行常用校验流程
