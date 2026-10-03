# 任务奖励配置

管理接口沿用原有任务管理权限和 POST/PUT 路径。`rewards` 必须是 JSON 对象；
提供 `currencies` 时必须是币种到正整数金额的对象，数组和字符串不能作为空奖励被接受。
币种仍需存在且活动，积分及经验限制沿用现有规则。仅提供有效正经验时，省略或设置
`currencies: null` 仍兼容。`rewards: null` 返回 400，不进入数据库写入。

`TestOCT02TaskRewardMalformedObjectsAreRejectedBeforePersistence` 验证结构错误在持久化前拒绝；现有任务配置数据库测试
继续验证币种、奖励和持久化语义。这些验证不改变任务领取资格或运营奖励数额。
