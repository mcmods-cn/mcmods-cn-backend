# 内容派生统计任务

`content_stats_refresh_queue` 和 `comment_heat_refresh_queue` 是已有的 PostgreSQL 合并队列，`SKIP LOCKED` 领取并递增 attempts，任务超时与退避沿用既有配置。

重建事务先 `FOR UPDATE` 锁定对应队列行，核对 attempts 和非空 locked_at，之后才执行指标/热度函数并删除该次领取的任务。新入队、重领和失效必须等待此事务；已经失效或被较新领取取代的旧 worker 不写入派生统计。任务重建失败回滚，再按原 attempts 条件退避，不删除较新任务。

派生结果可由数据库已有 refresh 函数重建，不能把队列清空当成主数据已正确或生产统计已核对。审计真实隔离 PostgreSQL 用不同 attempts 和人工合成计数验证旧领取不会覆盖、当前领取能完成重建；这不是生产运行容量或完整热度质量验证。
