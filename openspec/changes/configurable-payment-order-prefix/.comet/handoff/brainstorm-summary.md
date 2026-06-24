# Brainstorm Summary

- Change: configurable-payment-order-prefix
- Date: 2026-06-24

## 确认的技术方案

沿用现有 `PaymentConfigService` 设置机制新增商户订单号前缀配置。新增设置键保存前缀，`PaymentConfig` 和 `UpdatePaymentConfigRequest` 暴露该字段，后台设置页通过现有 settings 接口读写。创建支付订单时，`PaymentService` 使用已加载的 `PaymentConfig` 生成 `out_trade_no`，格式为 `<prefix><yyyyMMdd><8-char-random>`。

默认前缀保持 `sub2_`，因此未配置或空值不会改变现有部署行为。

## 关键取舍与风险

- 不由前端创建订单时传入前缀，避免把商户订单号规则放到客户端输入边界。
- 不新增数据库表，沿用项目已有 payment settings 模式，减少迁移和维护成本。
- 前缀 trim 后限制为 1-16 位，只允许 ASCII 字母、数字、下划线和短横线，降低支付渠道兼容风险。
- 已创建订单按完整 `out_trade_no` 查库，后续修改配置只影响新订单。

## 测试策略

- 后端覆盖默认前缀、自定义前缀、非法前缀拒绝、空值回退默认值，以及唯一性重试仍使用同一配置前缀。
- 后端覆盖 admin settings 和 admin payment config 的读写 DTO 映射。
- 前端覆盖设置页加载默认值、编辑保存 payload、类型和 i18n 更新。
- 运行 OpenSpec 校验和 targeted backend/frontend tests。

## Spec Patch

无。当前 `openspec/changes/configurable-payment-order-prefix/specs/payment/spec.md` 已覆盖默认、自定义、非法值和历史订单回调场景。
