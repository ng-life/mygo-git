# 项目开发说明

MyGo Git 是基于 Go 和 MyGo 的原生桌面 Git 客户端，管理多个本地仓库。启动、功能和用户配置说明见 `README.md`；Go 版本和依赖以 `go.mod` 为准。

## 开发

- 修改 MyGo 界面、排查崩溃或升级依赖前，阅读 `.agents/skills/mygo-maintenance/SKILL.md`，遵循其中的生命周期、线程和测试规则。
- 根目录的 `*_ui.go` 主要负责界面，`*_state.go` 负责状态；相关业务实现与 `*_test.go` 测试通常按功能命名并放在根目录。修改前沿调用链确认实际职责。
- 保留用户已有的工作区改动；Git 操作应保护所管理仓库中的未提交内容和进行中的操作。
- Java 依赖优先使用 Maven，Node.js 依赖优先使用 pnpm，Python 依赖优先使用 uv，其他开发语言工具优先使用 mise。Web 原型和前端开发基于 Ant Design Pro 设计。

## 验证与本地重启

- Go 代码修改后执行 `gofmt`、适当的测试和 `go tool mygo vet .`；共享状态改动增加 race 检查。完整测试命令为 `CGO_ENABLED=0 go test ./...`。
- 每次功能开发完成并检查后，运行 `./scripts/dev-restart.sh` 自动构建并重启本地客户端，确认新 PID 存活后交由用户人工验证；不能只构建而不重启。
- 重启脚本只停止本 checkout 的程序。发现 Git / SSH 子进程时等待操作完成，不强杀 Git，也不强制丢弃用户的工作区内容。
- 构建失败时保留原进程；重启失败时报告日志位置。PID 和日志分别位于 `bin/dev.pid`、`bin/dev.log`。
- 人工截图、运行日志、打包产物和个人配置不提交到仓库。
