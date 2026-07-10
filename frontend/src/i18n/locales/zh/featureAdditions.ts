// Restores feature-branch messages that predate the locale module split.
export default {
  "nav": {
    "imageStudio": "生图体验",
    "myUsageCards": "我的余额卡",
    "usageCardManagement": "余额卡管理"
  },
  "usageCards": {
    "title": "余额卡",
    "myTitle": "我的余额卡",
    "description": "查看和管理我的余额卡",
    "buy": "购买余额卡",
    "loading": "加载中...",
    "empty": "暂无余额卡",
    "noPurchasable": "暂无可购买的余额卡",
    "allStatus": "全部",
    "recentRedeemed": "最近兑换 {count} 张",
    "availableCount": "可用 {count} 张",
    "availableSummary": "可用余额 ${amount}",
    "viewMore": "查看更多",
    "remainingQuota": "剩余额度",
    "remaining": "剩余",
    "total": "总额",
    "availableQuota": "可用额度",
    "expiresAt": "{time} 到期",
    "redeemedAt": "{time} 兑换",
    "validDays": "{days} 天",
    "validForDays": "{days} 天有效",
    "status": {
      "active": "生效中",
      "exhausted": "已用完",
      "expired": "已过期",
      "suspended": "已暂停",
      "cancelled": "已撤销"
    },
    "source": {
      "payment": "支付购买",
      "redeem": "兑换码",
      "admin": "管理员发放",
      "migration": "迁移"
    }
  },
  "keys": {
    "billingPriority": {
      "label": "扣费优先级",
      "shortLabel": "扣费规则",
      "quickSwitch": "快速切换扣费优先级",
      "hint": "每个 API Key 可单独指定扣余额还是余额卡；不拆单。",
      "groupBalanceOnly": "该分组只能使用余额",
      "groupBalanceOnlyHint": "该分组只支持余额。",
      "updateSuccess": "扣费优先级已更新",
      "updateFailed": "更新扣费优先级失败",
      "options": {
        "auto": "跟随系统默认",
        "defaultRule": "{rule}（默认）",
        "balanceFirst": "余额优先",
        "usageCardFirst": "余额卡优先",
        "balanceOnly": "只扣余额",
        "usageCardOnly": "只扣余额卡"
      }
    }
  },
  "usage": {
    "deductionSource": "扣费来源",
    "deductionSources": {
      "balance": "余额",
      "subscription": "订阅",
      "usageCard": "余额卡"
    }
  },
  "imageStudio": {
    "title": "生图体验",
    "description": "使用当前账号的 API Key 调用 Sub2API 生图能力。"
  },
  "admin": {
    "usageCards": {
      "title": "余额卡管理",
      "addPlan": "新增套餐",
      "plans": "套餐",
      "userCards": "用户余额卡",
      "name": "名称",
      "price": "价格",
      "amount": "额度",
      "validity": "有效期",
      "forSale": "上架",
      "user": "用户",
      "remainingTotal": "剩余/总额",
      "status": "状态",
      "expiresAt": "到期时间",
      "actions": "操作",
      "allStatus": "全部状态",
      "revokedStatus": "已撤销",
      "userCardsHint": "默认展示生效中的用户余额卡，可按用户邮箱、用户名和状态筛选。",
      "searchUserPlaceholder": "搜索用户邮箱或用户名",
      "used": "已用",
      "remaining": "剩余",
      "total": "总额",
      "noUsername": "未设置用户名",
      "failedToLoadCards": "加载用户余额卡失败",
      "days": "{days} 天",
      "updating": "更新中",
      "onSale": "销售中",
      "offSale": "已下架",
      "saleOnTitle": "点击下架，用户购买页将不再展示该套餐",
      "saleOffTitle": "点击上架，用户购买页可展示该套餐",
      "editPlan": "编辑余额卡套餐",
      "newPlan": "新增余额卡套餐",
      "description": "余额卡套餐是用户在“充值&订阅”里购买的商品。用户支付“售价”后，会获得一张可在任意分组扣费的余额卡，最多可消费到“可用额度”。",
      "planName": "套餐名称",
      "planNamePlaceholder": "例如：100 万日卡、$20 余额卡",
      "planNameHint": "展示给用户看的商品名，建议包含额度或有效期。",
      "productName": "支付商品名称",
      "productNamePlaceholder": "例如：Credit Booster",
      "productNameHint": "仅用于支付渠道里的商品标题；留空时使用默认标题。",
      "planDescription": "套餐描述",
      "planDescriptionPlaceholder": "例如：适合临时高峰使用，购买后 30 天内有效",
      "planDescriptionHint": "会展示在购买卡片上，可写适用场景、限制或售后说明。",
      "paymentAmount": "支付金额",
      "priceHint": "用户实际支付的金额，单位跟当前支付系统一致。",
      "availableAmount": "可用额度",
      "billingUsage": "扣费用量",
      "amountHint": "用户最多能从这张卡扣掉多少美元成本，不是售价。",
      "validityDays": "有效天数",
      "dayUnit": "天",
      "validityHint": "从购买成功发卡时开始计算，到期后不再参与扣费。",
      "sortOrder": "排序",
      "sortHint": "数字越小越靠前；同数字按创建顺序排列。",
      "moveUp": "上移",
      "moveDown": "下移",
      "orderUpdated": "排序已更新",
      "features": "权益说明",
      "featuresPlaceholder": "可选。每行一条，例如：任意分组可用",
      "featuresHint": "预留展示字段，当前可先留空。",
      "saleEnabled": "上架销售",
      "saleEnabledHint": "开启后，且系统“允许购买余额卡”开关打开时，用户能在购买页看到这个套餐。",
      "saving": "保存中...",
      "saved": "保存成功",
      "saveFailed": "保存失败",
      "updateFailed": "更新失败",
      "saleEnabledSuccess": "已上架销售",
      "saleDisabledSuccess": "已下架",
      "suspend": "暂停",
      "resume": "恢复",
      "cancel": "撤销",
      "noActions": "无操作",
      "validation": {
        "nameRequired": "请填写套餐名称。",
        "productNameMaxLength": "支付商品名称不能超过 100 个字符。",
        "pricePositive": "售价必须大于 0。",
        "amountPositive": "可用额度必须大于 0。",
        "validityPositiveInteger": "有效天数必须是大于 0 的整数。",
        "sortNonNegativeInteger": "排序必须是大于等于 0 的整数。"
      }
    },
    "users": {
      "typeUsageCard": "余额卡",
      "balancePurchased": "余额充值购买",
      "subscriptionPurchased": "订阅购买",
      "usageCardPurchased": "余额卡购买",
      "usageCardRedeemed": "余额卡兑换",
      "sourcePurchase": "站内购买",
      "sourceRedeemCode": "兑换码兑换"
    },
    "groups": {
      "form": {
        "usageCardDisabled": "禁用余额卡扣费",
        "usageCardDisabledHint": "开启后，该普通分组只能扣用户余额；用户创建 API Key 时扣费优先级会锁定为余额优先。"
      }
    },
    "channelMonitor": {
      "form": {
        "endpointModeCustom": "自定义地址",
        "endpointModeCustomHint": "手动填写公网 HTTPS 地址，沿用现有监控方式。",
        "endpointModeSelf": "监控本站",
        "endpointModeSelfHint": "不填写地址，由后端固定调用本站内部地址。",
        "endpointModeSelfDesc": "本站监控模式会自动使用服务端固定的本地地址发起检测，不开放任意 localhost 或内网地址输入。"
      }
    },
    "accounts": {
      "openai": {
        "imageProtocolPreference": "生图接口偏好",
        "imageProtocolPreferenceDesc": "用于后台生图账号选择。优先匹配当前请求协议，自动时走现有转换逻辑。",
        "imageProtocolAuto": "自动",
        "imageProtocolImages": "Images",
        "imageProtocolResponses": "Responses"
      }
    },
    "redeem": {
      "types": {
        "usage_card": "余额卡"
      },
      "usageCard": "余额卡",
      "usageCardHint": "请选择已创建的余额卡套餐；下方过期时间只控制兑换码本身。",
      "selectUsageCardPlan": "选择余额卡套餐",
      "selectUsageCardPlanPlaceholder": "请选择余额卡套餐",
      "usageCardPlanRequired": "请选择余额卡套餐",
      "usageCardPlanSummary": "额度 ${amount}，有效期 {days} 天，套餐价格 ${price}。",
      "usageCardAmount": "余额卡额度 ($)",
      "usageCardDefaultValidity": "默认有效期",
      "usageCardAmountRequired": "请输入有效的余额卡额度"
    },
    "settings": {
      "tabs": {
        "imageStudio": "生图设置"
      },
      "imageStudio": {
        "title": "生图设置",
        "description": "管理站内 Image Studio 异步生图队列的并发和文件保留时长。",
        "asyncConcurrency": "生图异步并发",
        "asyncConcurrencyHint": "站内 Image Studio 队列的全站并发上限。建议从 1-3 开始。",
        "retention": "生图保存时长",
        "retentionHint": "0 表示永不过期；到期后仅清理原图和缩略图文件，任务记录保留。",
        "hours": "小时",
        "days": "天",
        "availableGroups": "生图体验可用分组",
        "availableGroupsHint": "只有选中分组下的 API Key 才能在生图体验中创建任务。未选择时，所有分组都不可用。",
        "availableGroupsEmpty": "暂无可选分组",
        "toolDeclarationPolicy": "image_generation 工具声明策略",
        "toolDeclarationPolicyHint": "控制客户端仅声明 image_generation 工具但未实际选择时的处理方式；实际生图仍受分组生图开关限制。",
        "toolDeclarationPolicyStrip": "移除被动声明并继续",
        "toolDeclarationPolicyAllow": "允许声明",
        "toolDeclarationPolicyReject": "拒绝声明"
      },
      "features": {
        "openaiLongContextBilling": {
          "title": "OpenAI GPT 长上下文计费",
          "description": "配置 GPT-5.4 / GPT-5.5 请求超过上下文阈值后的整次会话加价规则。默认保持旧规则：超过 272000 token 后输入 2 倍、输出 1.5 倍。",
          "enabled": "启用长上下文加价",
          "enabledHint": "关闭后 GPT-5.4 / GPT-5.5 使用普通计费，不再按长上下文阈值加倍率。",
          "threshold": "触发阈值（token）",
          "thresholdHint": "输入 token 总量超过该值时触发长上下文计费。",
          "inputMultiplier": "输入倍率",
          "inputMultiplierHint": "触发后整次会话的输入、缓存读取和缓存写入按该倍率计费。",
          "outputMultiplier": "输出倍率",
          "outputMultiplierHint": "触发后整次会话的文本输出按该倍率计费。"
        }
      },
      "defaults": {
        "defaultUsageCards": "默认余额卡",
        "defaultUsageCardsHint": "新用户创建或注册时自动发放这些余额卡套餐",
        "addDefaultUsageCard": "添加默认余额卡",
        "defaultUsageCardsEmpty": "未配置默认余额卡。新用户不会自动获得余额卡。",
        "defaultUsageCardsDuplicate": "默认余额卡存在重复套餐：{planId}。每个套餐只能出现一次。",
        "usageCardPlan": "余额卡套餐",
        "usageCardQuantity": "发放数量"
      },
      "site": {
        "legacySubscriptionPurchaseEnabled": "允许旧订阅购买",
        "legacySubscriptionPurchaseEnabledHint": "关闭后隐藏充值/订阅页里的订阅购买，并禁止创建旧订阅订单",
        "legacySubscriptionVisible": "显示旧订阅入口",
        "legacySubscriptionVisibleHint": "关闭后隐藏用户订阅页和后台订阅管理入口，直接访问也会回到仪表板",
        "usageCardEnabled": "启用余额卡",
        "usageCardEnabledHint": "总开关，关闭后用户不可查看或使用余额卡",
        "usageCardPaymentEnabled": "允许购买余额卡",
        "usageCardPaymentEnabledHint": "控制支付页余额卡购买入口和下单",
        "usageCardRedeemEnabled": "允许兑换余额卡",
        "usageCardRedeemEnabledHint": "兑换码可发放余额卡",
        "usageCardBillingEnabled": "启用余额卡扣费",
        "usageCardBillingEnabledHint": "网关扣费入口开始按优先级选择余额卡"
      },
      "payment": {
        "merchantOrderPrefix": "商户订单号前缀",
        "merchantOrderPrefixHint": "用于新支付订单的商户订单号前缀，仅支持字母、数字、下划线和短横线。"
      }
    }
  },
  "payment": {
    "admin": {
      "usageCardOrder": "余额卡"
    }
  }
} as const
