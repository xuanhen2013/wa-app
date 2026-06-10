# wa-app

`wa-app` 是 WA 应用链路的应用层服务，把账号、注册、登录态、消息会话和协议工具能力封装为 Go 原生原子 RPC 与 dashboard 能力。

> [!CAUTION]
> 使用本项目即表示你同意 [NOTICE](./NOTICE) 的全部条款。本项目仅限协议建模、教学演示、授权安全研究和内部非商业验证；禁止用于商业用途、未授权目标或违反第三方服务条款的场景。

## 核心能力

- 管理 WAAccount、客户端 profile、注册记录、登录态投影和消息元数据。
- 提供号码探测、验证码请求/提交、登录态检测、长连接会话、消息接收与 ack 等原子能力。
- 从消息中提取 OTP/Flag 候选值，并按敏感数据规则保存引用或脱敏投影。
- 提供 detached tooling，用于协议材料建模、请求材料构造和已验证参考资产导入。
- 自带 WA 管理 dashboard，覆盖账号管理、号码探测、注册动作和连接状态观察。

## 使用方式

业务侧通过 proto/gRPC 或 dashboard HTTP 边界调用 `wa-app-service`。长期事实优先进入 PG；短期运行态、锁和幂等窗口优先进入 Redis；未配置 PG/Redis 时降级到本服务 SQLite。`wa-re` 与 `app-release-re` 仅作为参考材料，不作为运行时桥接脚本。

## 入口

- 服务入口：`cmd/wa-app-service/`
- 契约真源：`proto/byte/v/forge/waapp/v1/`
- 应用实现：`internal/app/`
- 数据迁移：`migrations/`
- 前端模块：`webui/`
- 建模说明：`docs/modeling.md`

## 常用检查

```sh
scripts/generate-proto.sh
(cd webui && npm run proto)
git diff --check
```

## Docker 部署（推荐）

本仓库新增了 `docker-compose.yml` 与 `.env.example`，用于直接通过 GHCR 镜像启动服务。

```sh
# 1. 复制环境变量模板并按需修改
cp .env.example .env

# 2. 拉取镜像并启动
docker compose pull
docker compose up -d
```

`.env` 中可通过 `WA_APP_IMAGE_TAG` 控制镜像版本。建议：
- `latest`：快速验证（取决于远端 latest 标签）
- `sha-<短hash>` 或固定版本号：生产更推荐固定版本，避免漂移

常用维护命令：

```sh
docker compose logs -f wa-app
docker compose down
```

如果镜像仓库是私有的，在部署机上执行一次登录：

```sh
echo "<token>" | docker login ghcr.io -u <github-username> --password-stdin
```
