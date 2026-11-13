# Project instructions

- 遵循 `.agents/skills/mygo-maintenance/SKILL.md` 的 MyGo 生命周期、线程和测试规则。
- Go 代码修改后执行 gofmt、适当测试以及 `go tool mygo vet .`；共享状态改动增加 race 检查。
- 每次功能开发完成后，必须运行 `./scripts/dev-restart.sh` 自动构建并重启本地客户端，确认新 PID 存活，交由用户人工验证；不能只构建而不重启。
- 重启脚本只停止本 checkout 的程序。发现 Git / SSH 子进程时等待操作完成，不强杀 Git，也不强制丢弃用户的工作区内容。
- 构建失败时保留原进程；重启失败时报告日志位置。
- 人工截图、运行日志、打包产物和个人配置不提交到仓库。
