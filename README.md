# UAV Camera Calibration Scanner

每天扫描前一日 `09:00-17:00` 的 M30T/M30TD 历史属性数据，寻找最早一个连续 20s 满足规则的片段，然后调用 VOD clip 接口并保存 `clip_start` 时刻的完整控制中心属性 JSON。

当前实现优先从 control-center 动态获取设备列表，再走 DBAPI/Doris 历史属性路径做飞参过滤；Kafka 只作为后续 fallback/backfill。
命中并拿到 clip 后，可以继续调用 Auto_photograph 的 `/calibration/create` 创建标定任务。

## Run

```bash
go test ./...
go run ./cmd/uav-camera-calibration-scanner -config configs/config.example.json -mode once -date 2026-07-06 -dry-run
go run ./cmd/uav-camera-calibration-scanner -config configs/config.yml -mode schedule
```

`configs/config.yml` 是部署主配置，已经把必需项、默认扫描窗口、日志、背压、VOD、标定、结果输出都显式写出。`configs/config.example.json` 保留为本地样本配置，默认使用静态设备列表、本地样本文件和 VOD dry-run，可以直接跑通本地闭环。

生产环境需要配置：

- `controlCenter.enabled=true`
- `controlCenter.baseUrl`，例如 `http://ja-control-center.jingqi.svc.cluster.local:32041`
- `controlCenter.deviceListPath=/openapi/device/list`
- `controlCenter.username=admin` 和 `controlCenter.groupId=1`
- `controlCenter.candidateProductKeys`，用于先按产品线粗过滤设备
- `dbapi.baseUrl`
- `dbapi.historyPath`，默认 `/deviceAttrInfoBack`
- `dbapi.snapshotPath`，默认 `/uavDeviceAttrBackV2`
- 清空 `dbapi.sampleFile`，否则会走本地样本文件
- `vod.baseUrl` 与 `vod.clipPath`
- 测试/生产开启真实 VOD 时设置 `vod.dryRun=false`，且运行命令不要带 `-dry-run`
- `calibration.enabled=true` 和 `calibration.baseUrl`，开启后会把 VOD 返回的 `clipUrl` 作为 `video_url` 提交给标定系统
- `calibration.dedupEnabled=true` 和 `calibration.dedupDays=7`，同一台无人机最近 7 天已有成功标定任务时，会在 DBAPI 扫描前跳过
- `result.mysqlDsn` 或 `result.jsonlPath`

命令行 `-dry-run` 会强制 VOD dry-run，并同时关闭标定外呼。
`-mode once` 执行一次后退出；`-mode schedule` 按 `schedule.dailyAt` 每天执行，生产环境建议不要传 `-date`，让服务固定扫描前一天。
当前公网测试标定服务地址可填 `http://121.196.145.43:11595`；开启前需要确认该容器可以访问对象存储，否则 `/calibration/create` 会卡在视频下载阶段。

如果结果表已经存在，先执行 `sql/20260707_add_calibration_columns.sql` 增加标定结果字段；新建表可直接用 `sql/uav_downward_photo_clip.sql`。

程序支持 `.json`、`.yml`、`.yaml` 三种配置文件；生产部署建议统一使用 `configs/config.yml`。

## Docker / Release

镜像默认以非 root 用户运行，入口命令是 `uav-camera-calibration-scanner -config /app/configs/config.yml -mode schedule`。更推荐用外部配置文件挂载覆盖镜像内置示例：

```bash
make print-image
make release
make docker-push-amd
make docker-up CONFIG=configs/test-env-s220-20260708.yml
make docker-logs
```

Docker tag 规则参考 `ja-dushu-front`：默认使用 `registry.jingan.com:32008/ja/uav-camera-calibration-scanner:<VERSION>.<yymmdd>`，arm64 镜像追加 `-arm`。`VERSION` 来自仓库根目录的 `VERSION` 文件，例如 `v0.1.0.260708`。

对应目标：

```bash
make docker-build-amd          # registry.jingan.com:32008/ja/...:<VERSION>.<yymmdd>
make docker-build-arm          # registry.jingan.com:32008/ja/...:<VERSION>.<yymmdd>-arm
make docker-build-public       # registry.cn-hangzhou.aliyuncs.com/jingansi/...:<VERSION>.<yymmdd>
make docker-build-public-arm   # registry.cn-hangzhou.aliyuncs.com/jingansi/...:<VERSION>.<yymmdd>-arm
```

`.docker/.docker-build.sh` 会按上述顺序构建并推送内网/公网 amd64/arm64 镜像。没有 registry 权限时仍可使用 `make docker-save` 导出 tar 包后手动传到测试机。

Release checklist 是硬性前置项：`make release` 会先执行 `make release-checklist`，其中包含 `go test ./...` 和一次集成测试；任一失败都不会进入 Docker build。默认集成测试使用本地样本配置：

```bash
make release-checklist
```

要在测试环境跑同一项集成测试，可覆盖配置和日期：

```bash
make release-checklist \
  INTEGRATION_CONFIG=configs/test-env-s220-20260708.yml \
  INTEGRATION_DATE=2026-07-08 \
  INTEGRATION_ARGS=-dry-run
```

本地一次性验证：

```bash
make docker-run-once IMAGE=uav-camera-calibration-scanner:latest CONFIG=configs/config.yml DATE=2026-07-07
```

`docker-compose.yml` 会挂载：

- `./configs/config.yml:/config/config.yml:ro`
- `./out/docker:/app/out/docker`

容器以 UID `10001` 的非 root 用户运行。宿主机挂载目录需要可写：

```bash
mkdir -p out/docker/{artifacts,results,run}
chown -R 10001:10001 out/docker
```

生产发布时复制 `configs/config.yml` 为环境专用配置，填入 control-center、DBAPI、VOD、结果表和对象存储可访问路径；标定默认关闭，确认链路可用后再设置 `calibration.enabled=true`。

## Helm / Kubernetes

Helm chart 位于 `deploy/helm/uav-camera-calibration-scanner`。K8s 部署方式保持和 Docker 一致：运行一个常驻 Deployment，应用内部按 `schedule.dailyAt` 定时扫描；PVC 挂载到 `/app/out/docker`，保存运行锁、属性文件、JSONL 结果和 7 天去重历史。

```bash
make helm-lint
make helm-template
make helm-upgrade HELM_NAMESPACE=ja \
  HELM_VALUES=deploy/helm/uav-camera-calibration-scanner/values-test.yaml
```

默认镜像 tag 会跟随 Makefile 的 `VERSION + yymmdd` 规则，也可以显式覆盖：

```bash
helm upgrade --install uav-camera-calibration-scanner deploy/helm/uav-camera-calibration-scanner \
  -n ja --create-namespace \
  -f deploy/helm/uav-camera-calibration-scanner/values-test.yaml \
  --set image.tag=v0.1.0.260708
```

如果集群拉私有仓库需要凭证，在 values 中配置 `imagePullSecrets`。默认安全上下文使用 UID/GID `10001` 和 `fsGroup=10001`，PVC 不需要手工 `chown`。

## Logging

服务使用 `log/slog` 输出结构化日志到 stdout，方便 Docker / Kubernetes / 日志采集器直接收集。默认配置：

```yaml
logging:
  level: info
  format: json
```

`level` 支持 `debug`、`info`、`warn`、`error`；`format` 支持 `json` 和 `text`。关键日志包括扫描窗口、目标设备数量、命中设备、调度下一次运行、外部依赖失败、熔断打开、运行锁跳过/陈旧锁清理等。

## Backpressure / Resilience

当前背压策略偏保守，优先保护 control-center、DBAPI、VOD 和标定服务：

- 每次扫描只调用一次 control-center 设备列表接口，拿到候选设备后再逐台查询历史飞参。
- 扫描按 `scan.queryChunkMinutes` 分片查询，默认 30 分钟一段，避免一次拉取全天大结果集。
- 设备、时间分片、VOD getclip、标定创建全部串行执行，保证“最早命中”语义，也避免把下游服务打满。
- 单台设备命中后立即停止该设备后续分片查询；未命中时才扫完整个时间窗用于落 `NO_MATCH`。
- `resilience.lockFile` 保证同一实例不会重入扫描；锁超过 `runTimeoutMinutes + 10` 分钟会按陈旧锁清理，避免容器被强杀后永久跳过。
- `resilience.runTimeoutMinutes` 给单次扫描设置总超时，默认 120 分钟。
- ControlCenter、DBAPI、VOD、Calibration 分别有独立熔断器；连续失败达到 `circuitFailureThreshold` 后打开 `circuitOpenSeconds`，期间失败快速返回。
- `retryAttempts` 是单次外部调用的最大尝试次数，包含首次调用；`retryBaseDelayMillis` 使用指数退避。

示例配置：

```yaml
resilience:
  retryAttempts: 2
  retryBaseDelayMillis: 500
  circuitFailureThreshold: 3
  circuitOpenSeconds: 300
  runTimeoutMinutes: 120
  lockFile: out/docker/run/uav-camera-calibration-scanner.lock
```

## Rule

连续 20s 内样本都必须满足：

- `cameraMode=0`
- `gimbalType` 必须命中 `rule.targetGimbalTypes`；当前生产配置只允许 `M30T_CAMERA`、`M30TD_CAMERA`
- `gimbalPitch` 在 `[-90, -10]`
- `lensType=wide`
- `height >= minHeightMeters`，默认 `5m`
- `flyTime > 0`，默认开启
- 20s 窗口内起止经纬度直线距离 `>= minTravelMeters`，默认 `100m`，用于排除悬停片段

`targetGimbalTypes` 来自 DBAPI 历史属性里的 `properties.gimbalType` / `properties.gimbal_type`，并会做归一化匹配，所以 `M30T_CAMERA`、`M30T Camera`、`m30t-camera` 会视为同一种类型。`flightStatus` 默认不参与判定；如果需要兼容旧口径，可显式开启 `requireFlightStatus`，开启后会排除空值和 `standby`。缺样间隔超过 `maxSampleGapSeconds` 会拆段；缺少经纬度或 20s 内位移不足 `minTravelMeters` 不会命中；每台设备每天只取最早命中段。

## Scan Strategy

扫描不会一次性拉取整天 `09:00-17:00` 的全部历史属性。服务会按 `scan.queryChunkMinutes` 分片查询，默认每 `30` 分钟一段；每段末尾会额外向后查询一个规则窗口 overlap，避免漏掉跨分片边界的 20s 片段。某台设备一旦在较早分片中命中并完成 VOD/标定创建，就停止该设备后续分片查询。

设备列表阶段优先从 control-center `/openapi/device/list` 动态获取平台内设备，请求头使用 `username: admin` 和 `groupId: 1`；`controlCenter.candidateProductKeys` 只做产品线粗过滤，避免漏掉新上线设备。control-center 不可用或返回空列表时，才依次 fallback 到 `dbapi.deviceListPath` 和静态 `devices`。

候选设备仍会按 `targetModels` 匹配 `device.model` 或 `device.tags`。如果配置了 `rule.targetGimbalTypes`，即使设备列表没有目标机型标签，也会先作为候选设备进入历史属性扫描，再由 DBAPI 飞参里的 `gimbalType` 做最终过滤。

生产配置开启 `scan.preciseDevicePrefilter=true`。设备候选会先使用 control-center 里的显式信号收敛：`device.model`、`tags.GIMBAL_TYPE`、`tags.DEVICE_MODEL`、`tags.DEVICE_TYPE`，以及 `deviceName` 中包含的 `M30T/M30TD` token；最终再以 DBAPI 历史飞参中的 `gimbalType` 做一次严格校验。其他机型不会进入历史扫描。

如果当天没有命中，服务仍会按分片逐段扫完整个配置时间窗；这是为了证明 `NO_MATCH`，但不会单次拉回全天大结果集。

## Concurrency Model

当前实现是**串行扫描、串行 getclip、串行标定创建**：

- 设备之间不并发：服务按设备列表顺序逐台扫描。
- 单台设备内不并发：按时间分片从早到晚查询 DBAPI，保证“最早合格窗口”语义。
- VOD getclip 不并发：某台设备命中后才调用一次 VOD clip。
- 标定创建不并发：VOD 成功后才调用一次 Auto_photograph `/calibration/create`。

这样做是为了避免 DBAPI、VOD 和 NodeODM/标定服务被定时任务打满。尤其当前 NodeODM `maxParallelTasks=1`，标定并发提交只会排队或放大资源压力，暂不建议并发标定。

## VOD Clip

规则判定窗口由 `rule.durationSeconds` 控制，默认连续满足 20s。实际向 VOD 请求的视频长度由 `vod.clipDurationSeconds` 控制；测试环境当前设为 40s，用来覆盖历史 TS 分片存在间隔时只截到十几秒有效媒体的问题。

## Property Artifact

每个命中段只落一份 JSON 文件，结构为：

- `meta`: 设备、clip 时间、快照时间、规则版本。
- `flightParams`: 标定/排查用飞参摘要，必须包含 `cameraSn` 或 `sn`、`gimbalType`、`cameraMode`。
- `properties`: `clip_start` 时刻完整控制中心属性 JSON。

`flightParams.cameraMode` 按控制中心原始值保存，不做 `photo/video` 映射，也不改 JSON 类型。缺少 `cameraSn/sn`、`gimbalType` 或 `cameraMode` 时不会生成成功结果。

## Calibration

开启 `calibration.enabled` 后，服务会在 VOD clip 成功后调用：

```http
POST /calibration/create
```

请求体字段：

- `video_url`: VOD 返回的 clip 地址。测试环境 VOD 当前返回 `/ja-media-storage/...mp4`，Auto_photograph 会按 MinIO 的 `bucket/object` 路径下载。
- `uav_parameters`: `clip_start` 时刻完整控制中心属性 JSON 中当前设备对应的属性对象，保留原始字段和值。
- `fps`: `calibration.fps`，默认 `1.0`。
- `callback_url`: `calibration.callbackUrl`，为空时由 Auto_photograph 使用自身配置。

结果表会保存 `calibration_task_id`、`calibration_status`、`calibration_camera_sn`、`calibration_camera_mode` 和 `calibration_error`。标定创建失败时，clip 与属性文件仍会落地，结果行状态为 `FAILED_RETRYABLE`，便于重试或人工排查。

默认开启 7 天标定去重：如果同一 `device_id` 在结果历史中存在 `SUCCESS` 且 `calibration_task_id` 非空的记录，并且 `created_at` 距当前时间小于 `calibration.dedupDays`，服务会在 DBAPI 历史扫描前直接落 `SKIPPED_RECENT_CALIBRATION`，不再拉飞参、不调用 VOD、不提交标定。去重历史来自 MySQL 结果表或持久化 JSONL；如果两者都不持久，容器重建后无法严格保证去重。
