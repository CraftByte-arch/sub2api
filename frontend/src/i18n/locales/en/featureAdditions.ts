// Restores feature-branch messages that predate the locale module split.
export default {
  "nav": {
    "imageStudio": "Image Studio",
    "myUsageCards": "My Usage Cards",
    "usageCardManagement": "Usage Cards"
  },
  "usageCards": {
    "title": "Usage Cards",
    "myTitle": "My Usage Cards",
    "description": "View and manage my usage cards",
    "buy": "Buy Usage Card",
    "loading": "Loading...",
    "empty": "No usage cards",
    "noPurchasable": "No usage cards available for purchase",
    "allStatus": "All",
    "recentRedeemed": "Recently redeemed {count}",
    "availableCount": "{count} available",
    "availableSummary": "${amount} available",
    "viewMore": "View More",
    "remainingQuota": "Remaining Quota",
    "remaining": "Remaining",
    "total": "Total",
    "availableQuota": "Available Quota",
    "expiresAt": "Expires at {time}",
    "redeemedAt": "Redeemed at {time}",
    "validDays": "{days} days",
    "validForDays": "Valid for {days} days",
    "status": {
      "active": "Active",
      "exhausted": "Exhausted",
      "expired": "Expired",
      "suspended": "Suspended",
      "cancelled": "Revoked"
    },
    "source": {
      "payment": "Payment",
      "redeem": "Redeem Code",
      "admin": "Admin Grant",
      "migration": "Migration"
    }
  },
  "keys": {
    "billingPriority": {
      "label": "Billing Priority",
      "shortLabel": "Billing Rule",
      "quickSwitch": "Quick switch billing priority",
      "hint": "Each API key can choose whether to charge balance or usage cards first. No split billing.",
      "groupBalanceOnly": "This group can only use balance",
      "groupBalanceOnlyHint": "This group only supports balance.",
      "updateSuccess": "Billing priority updated",
      "updateFailed": "Failed to update billing priority",
      "options": {
        "auto": "Follow system default",
        "defaultRule": "{rule} (default)",
        "balanceFirst": "Balance first",
        "usageCardFirst": "Usage card first",
        "balanceOnly": "Balance only",
        "usageCardOnly": "Usage card only"
      }
    }
  },
  "usage": {
    "deductionSource": "Deduction Source",
    "deductionSources": {
      "balance": "Balance",
      "subscription": "Subscription",
      "usageCard": "Usage Card"
    }
  },
  "imageStudio": {
    "title": "Image Studio",
    "description": "Use your API keys to generate and edit images through Sub2API."
  },
  "admin": {
    "usageCards": {
      "title": "Usage Card Management",
      "addPlan": "New Plan",
      "plans": "Plans",
      "userCards": "User Usage Cards",
      "name": "Name",
      "price": "Price",
      "amount": "Amount",
      "validity": "Validity",
      "forSale": "For Sale",
      "user": "User",
      "remainingTotal": "Remaining / Total",
      "status": "Status",
      "expiresAt": "Expires At",
      "actions": "Actions",
      "allStatus": "All Status",
      "revokedStatus": "Revoked",
      "userCardsHint": "Active user usage cards are shown by default. Filter by user email, username, and status.",
      "searchUserPlaceholder": "Search email or username",
      "used": "Used",
      "remaining": "Remaining",
      "total": "Total",
      "noUsername": "No username",
      "failedToLoadCards": "Failed to load user usage cards",
      "days": "{days} days",
      "updating": "Updating",
      "onSale": "On Sale",
      "offSale": "Off Sale",
      "saleOnTitle": "Click to remove this plan from the user purchase page",
      "saleOffTitle": "Click to show this plan on the user purchase page",
      "editPlan": "Edit Usage Card Plan",
      "newPlan": "New Usage Card Plan",
      "description": "Usage card plans are products users buy from Recharge / Subscription. After payment, users receive a usage card that can be charged in any group up to the available amount.",
      "planName": "Plan Name",
      "planNamePlaceholder": "Example: 1M daily card, $20 usage card",
      "planNameHint": "Shown to users. Include amount or validity when possible.",
      "productName": "Payment Product Name",
      "productNamePlaceholder": "Example: Credit Booster",
      "productNameHint": "Only used as the payment-provider product title. Leave empty to use the default title.",
      "planDescription": "Plan Description",
      "planDescriptionPlaceholder": "Example: For temporary peak usage, valid for 30 days after purchase",
      "planDescriptionHint": "Shown on the purchase card. Describe use cases, limits, or support notes.",
      "paymentAmount": "Payment Amount",
      "priceHint": "The amount users pay, using the current payment system unit.",
      "availableAmount": "Available Amount",
      "billingUsage": "Billing Usage",
      "amountHint": "The maximum USD cost that can be deducted from this card, not the sale price.",
      "validityDays": "Validity Days",
      "dayUnit": "days",
      "validityHint": "Calculated from successful card issuance. Expired cards no longer participate in billing.",
      "sortOrder": "Sort Order",
      "sortHint": "Smaller numbers appear first; equal numbers use creation order.",
      "moveUp": "Move Up",
      "moveDown": "Move Down",
      "orderUpdated": "Order updated",
      "features": "Features",
      "featuresPlaceholder": "Optional. One per line, e.g. Available for any group",
      "featuresHint": "Reserved display field. It can be left empty for now.",
      "saleEnabled": "List for Sale",
      "saleEnabledHint": "When enabled, and the system purchase switch is on, users can see this plan on the purchase page.",
      "saving": "Saving...",
      "saved": "Saved",
      "saveFailed": "Save failed",
      "updateFailed": "Update failed",
      "saleEnabledSuccess": "Listed for sale",
      "saleDisabledSuccess": "Removed from sale",
      "suspend": "Suspend",
      "resume": "Resume",
      "cancel": "Revoke",
      "noActions": "No actions",
      "validation": {
        "nameRequired": "Please enter a plan name.",
        "productNameMaxLength": "Payment product name cannot exceed 100 characters.",
        "pricePositive": "Price must be greater than 0.",
        "amountPositive": "Available amount must be greater than 0.",
        "validityPositiveInteger": "Validity days must be an integer greater than 0.",
        "sortNonNegativeInteger": "Sort order must be a non-negative integer."
      }
    },
    "users": {
      "typeUsageCard": "Usage Card",
      "balancePurchased": "Balance Purchase",
      "subscriptionPurchased": "Subscription Purchase",
      "usageCardPurchased": "Usage Card Purchased",
      "usageCardRedeemed": "Usage Card Redeemed",
      "sourcePurchase": "Purchased on site",
      "sourceRedeemCode": "Redeemed via code"
    },
    "groups": {
      "form": {
        "usageCardDisabled": "Disable Usage Card Billing",
        "usageCardDisabledHint": "When enabled, this normal group can only charge user balance. API key billing priority will be locked to Balance First."
      }
    },
    "channelMonitor": {
      "form": {
        "endpointModeCustom": "Custom endpoint",
        "endpointModeCustomHint": "Enter a public HTTPS endpoint and use the existing monitor flow.",
        "endpointModeSelf": "Monitor this service",
        "endpointModeSelfHint": "No endpoint input. The backend uses a fixed internal self address.",
        "endpointModeSelfDesc": "Self-monitor mode always uses a fixed local address on the server side and does not open arbitrary localhost or private-network input."
      }
    },
    "accounts": {
      "openai": {
        "imageProtocolPreference": "Image API preference",
        "imageProtocolPreferenceDesc": "Used for image-generation account routing. Matching request protocol is preferred; auto keeps existing conversion behavior.",
        "imageProtocolAuto": "Auto",
        "imageProtocolImages": "Images",
        "imageProtocolResponses": "Responses"
      }
    },
    "redeem": {
      "usageCard": "Usage Card",
      "usageCardHint": "Select an existing usage card plan. The expiry below only controls the redeem code itself.",
      "selectUsageCardPlan": "Select Usage Card Plan",
      "selectUsageCardPlanPlaceholder": "Choose a usage card plan",
      "usageCardPlanRequired": "Please select a usage card plan",
      "usageCardPlanSummary": "Amount ${amount}, valid for {days} days, plan price ${price}.",
      "usageCardAmount": "Usage Card Amount ($)",
      "usageCardDefaultValidity": "Default validity",
      "types": {
        "usage_card": "Usage Card"
      },
      "usageCardAmountRequired": "Enter a valid usage card amount"
    },
    "settings": {
      "tabs": {
        "imageStudio": "Image Studio"
      },
      "imageStudio": {
        "title": "Image Studio Settings",
        "description": "Manage the site-wide async queue concurrency and file retention for Image Studio.",
        "asyncConcurrency": "Async image concurrency",
        "asyncConcurrencyHint": "The global concurrency limit for the internal Image Studio queue. Start with 1-3.",
        "retention": "Image retention",
        "retentionHint": "0 means never expire. When expired, only original and thumbnail files are deleted; job records remain.",
        "hours": "Hours",
        "days": "Days",
        "availableGroups": "Image Studio available groups",
        "availableGroupsHint": "Only API keys in selected groups can create Image Studio jobs. If none are selected, no groups are available.",
        "availableGroupsEmpty": "No groups available",
        "toolDeclarationPolicy": "image_generation tool declaration policy",
        "toolDeclarationPolicyHint": "Controls requests that only declare image_generation without explicitly selecting it; actual image generation remains gated by the group switch.",
        "toolDeclarationPolicyStrip": "Strip passive declaration and continue",
        "toolDeclarationPolicyAllow": "Allow declaration",
        "toolDeclarationPolicyReject": "Reject declaration"
      },
      "features": {
        "openaiLongContextBilling": {
          "title": "OpenAI GPT Long Context Billing",
          "description": "Configure the whole-session markup for GPT-5.4 / GPT-5.5 requests that exceed the context threshold. Default preserves the old rule: 2x input and 1.5x output after 272000 tokens.",
          "enabled": "Enable long-context markup",
          "enabledHint": "When off, GPT-5.4 / GPT-5.5 uses normal billing and ignores the long-context threshold.",
          "threshold": "Trigger threshold (tokens)",
          "thresholdHint": "Long-context billing is triggered when total input tokens exceed this value.",
          "inputMultiplier": "Input multiplier",
          "inputMultiplierHint": "When triggered, input, cache read, and cache write tokens for the whole session use this multiplier.",
          "outputMultiplier": "Output multiplier",
          "outputMultiplierHint": "When triggered, text output tokens for the whole session use this multiplier."
        }
      },
      "defaults": {
        "defaultUsageCards": "Default Usage Cards",
        "defaultUsageCardsHint": "Auto-issue these usage card plans when a new user is created or registered",
        "addDefaultUsageCard": "Add Default Usage Card",
        "defaultUsageCardsEmpty": "No default usage cards configured.",
        "defaultUsageCardsDuplicate": "Duplicate usage card plan: {planId}. Each plan can only appear once.",
        "usageCardPlan": "Usage Card Plan",
        "usageCardQuantity": "Quantity"
      },
      "site": {
        "legacySubscriptionPurchaseEnabled": "Allow legacy subscription purchases",
        "legacySubscriptionPurchaseEnabledHint": "When disabled, subscription purchase is hidden from Recharge / Subscription and legacy subscription orders are rejected",
        "legacySubscriptionVisible": "Show legacy subscription entries",
        "legacySubscriptionVisibleHint": "When disabled, user subscriptions and admin subscription management are hidden and direct visits return to the dashboard",
        "usageCardEnabled": "Enable Usage Cards",
        "usageCardEnabledHint": "Master switch. When disabled, users cannot view or use usage cards",
        "usageCardPaymentEnabled": "Allow Usage Card Purchases",
        "usageCardPaymentEnabledHint": "Controls the usage-card purchase entry and order creation on the payment page",
        "usageCardRedeemEnabled": "Allow Usage Card Redemption",
        "usageCardRedeemEnabledHint": "Redeem codes can issue usage cards",
        "usageCardBillingEnabled": "Enable Usage Card Billing",
        "usageCardBillingEnabledHint": "Gateway billing starts selecting usage cards according to priority"
      },
      "payment": {
        "merchantOrderPrefix": "Merchant Order Prefix",
        "merchantOrderPrefixHint": "Prefix for new payment merchant order numbers. Letters, numbers, underscore, and hyphen only."
      }
    }
  },
  "payment": {
    "admin": {
      "usageCardOrder": "Usage Card"
    }
  }
} as const
