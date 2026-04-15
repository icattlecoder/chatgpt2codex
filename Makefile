SHELL := /usr/bin/env bash

.DEFAULT_GOAL := help

GO ?= go
APP_NAME ?= chatgpt2codex
CMD_PATH ?= ./cmd/$(APP_NAME)
DIST_DIR ?= dist
ARGS ?=

LOCAL_BIN := bin/$(APP_NAME)
ifeq ($(OS),Windows_NT)
LOCAL_BIN := bin/$(APP_NAME).exe
endif

.PHONY: help fmt test build install release clean verify

help: ## 显示可用命令
	@awk 'BEGIN {FS = ":.*## "; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## 格式化 Go 代码
	$(GO) fmt ./...

test: ## 运行 Go 测试
	$(GO) test ./...

build: ## 构建本地 CLI 到 $(LOCAL_BIN)
	@mkdir -p "$(dir $(LOCAL_BIN))"
	CGO_ENABLED=0 $(GO) build -trimpath -o "$(LOCAL_BIN)" $(CMD_PATH)

install: ## 安装 CLI 到 GOBIN 或 GOPATH/bin
	CGO_ENABLED=0 $(GO) install $(CMD_PATH)

release: ## 构建 GitHub Release 产物到 $(DIST_DIR)
	bash scripts/build-release.sh "$(DIST_DIR)"

clean: ## 清理本地构建产物
	rm -f bin/$(APP_NAME) bin/$(APP_NAME).exe
	rm -rf "$(DIST_DIR)"

verify: fmt test build ## 运行常用校验流程
