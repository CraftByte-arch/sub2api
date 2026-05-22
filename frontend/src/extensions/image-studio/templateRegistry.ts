import type { ImagePromptTemplate } from './templateTypes'

export const imagePromptTemplates: ImagePromptTemplate[] = [
  {
    id: 'poster-minimal-cat',
    title: '极简海报',
    mode: 'text-to-image',
    section: 'common',
    category: '海报',
    description: '适合做高级、克制、留白感明显的品牌海报。',
    tags: ['海报', '极简', '品牌', '平面设计'],
    previewText: '蓝橙对比、留白构图、极简现代',
    recommendedRatios: ['1:1', '4:5', '3:4'],
    recommendedModel: 'gpt-image-2',
    badge: '品牌',
    promptFragments: [
      '为{topic}设计一张海报',
      '整体风格为{style}',
      '主体是{subject}',
      '整体配色以{color}为主',
      '光线氛围为{lighting}',
      '构图强调{composition}',
      '画面质感为{texture}',
      '避免出现{negative}'
    ],
    fields: [
      { key: 'topic', label: '主题', type: 'text', required: true, placeholder: '例如：宠物咖啡店开业海报', section: 'basic' },
      { key: 'subject', label: '主体', type: 'text', required: true, placeholder: '例如：一只慵懒的猫', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '极简现代',
        options: [
          { label: '极简现代', value: '极简现代' },
          { label: '高级时尚', value: '高级时尚' },
          { label: '艺术海报', value: '艺术海报' }
        ],
        section: 'basic'
      },
      { key: 'color', label: '主配色', type: 'text', defaultValue: '蓝橙对比色', section: 'basic' },
      {
        key: 'lighting',
        label: '光线',
        type: 'select',
        defaultValue: '柔和自然光',
        options: [
          { label: '柔和自然光', value: '柔和自然光' },
          { label: '电影感侧光', value: '电影感侧光' },
          { label: '高反差光影', value: '高反差光影' }
        ],
        section: 'advanced'
      },
      { key: 'composition', label: '构图', type: 'text', placeholder: '例如：大面积留白，主体偏右下', section: 'advanced' },
      { key: 'texture', label: '质感', type: 'text', placeholder: '例如：平面设计感、干净利落', section: 'advanced' },
      { key: 'negative', label: '避免元素', type: 'text', placeholder: '例如：杂乱背景、密集文字', section: 'advanced' }
    ]
  },
  {
    id: 'poster-film-key-visual',
    title: '活动主视觉海报',
    mode: 'text-to-image',
    section: 'common',
    category: '海报',
    description: '适合展览、发布会、节日活动等大画面主视觉海报。',
    tags: ['海报', '活动', 'KV', '主视觉'],
    previewText: '大标题氛围、强视觉冲击、统一品牌感',
    recommendedRatios: ['4:5', '3:4', '16:9'],
    recommendedModel: 'gpt-image-2',
    badge: '主视觉',
    promptFragments: [
      '为{event}设计一张活动主视觉海报',
      '画面主角为{subject}',
      '整体视觉风格为{style}',
      '主配色采用{color}',
      '强调{highlight}',
      '构图节奏偏向{composition}'
    ],
    fields: [
      { key: 'event', label: '活动主题', type: 'text', required: true, placeholder: '例如：春季品牌发布会', section: 'basic' },
      { key: 'subject', label: '视觉主体', type: 'text', required: true, placeholder: '例如：悬浮产品、抽象装置、人物剪影', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '现代高级',
        options: [
          { label: '现代高级', value: '现代高级' },
          { label: '电影感艺术', value: '电影感艺术' },
          { label: '未来科技', value: '未来科技' }
        ],
        section: 'basic'
      },
      { key: 'color', label: '主配色', type: 'text', defaultValue: '高饱和品牌撞色', section: 'basic' },
      { key: 'highlight', label: '重点信息', type: 'text', placeholder: '例如：发光主体、品牌符号、大标题区域', section: 'advanced' },
      { key: 'composition', label: '构图节奏', type: 'text', placeholder: '例如：中轴对称、大留白、强层次透视', section: 'advanced' }
    ]
  },
  {
    id: 'ecommerce-product-hero',
    title: '电商产品主图',
    mode: 'text-to-image',
    section: 'common',
    category: '商品图',
    description: '适合护肤品、数码、饮料等需要质感展示的电商主图。',
    tags: ['商品图', '电商', '产品', '广告'],
    previewText: '纯净背景、商业光线、突出产品细节',
    recommendedRatios: ['1:1', '4:5'],
    recommendedModel: 'gpt-image-2',
    badge: '转化',
    promptFragments: [
      '为{product}创作一张{style}风格的电商主图',
      '产品主体为{subject}',
      '背景设置为{scene}',
      '强调{sellingPoint}',
      '整体光线为{lighting}',
      '材质质感突出{texture}'
    ],
    fields: [
      { key: 'product', label: '产品类型', type: 'text', required: true, placeholder: '例如：护手霜新品', section: 'basic' },
      { key: 'subject', label: '主体描述', type: 'text', required: true, placeholder: '例如：一支白色极简铝管护手霜', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '高级商业广告',
        options: [
          { label: '高级商业广告', value: '高级商业广告' },
          { label: '清新自然', value: '清新自然' },
          { label: '科技质感', value: '科技质感' }
        ],
        section: 'basic'
      },
      { key: 'scene', label: '背景场景', type: 'text', defaultValue: '纯净浅色摄影棚背景', section: 'basic' },
      { key: 'sellingPoint', label: '卖点重点', type: 'text', placeholder: '例如：补水、轻盈、玻璃光泽', section: 'advanced' },
      { key: 'lighting', label: '光线', type: 'text', defaultValue: '柔和商业棚拍光', section: 'advanced' },
      { key: 'texture', label: '质感', type: 'text', defaultValue: '干净、通透、细节清晰', section: 'advanced' }
    ]
  },
  {
    id: 'product-lifestyle-scene',
    title: '产品生活方式场景图',
    mode: 'text-to-image',
    section: 'common',
    category: '商品图',
    description: '适合把产品放进真实使用场景，增强氛围和代入感。',
    tags: ['商品图', '生活方式', '场景图', '品牌广告'],
    previewText: '真实环境、品牌气质、产品与场景统一',
    recommendedRatios: ['4:5', '3:4'],
    recommendedModel: 'gpt-image-2',
    badge: '氛围',
    promptFragments: [
      '为{product}创作一张生活方式场景图',
      '主体是{subject}',
      '场景设置在{scene}',
      '整体风格为{style}',
      '突出{sellingPoint}',
      '光线氛围为{lighting}'
    ],
    fields: [
      { key: 'product', label: '产品类型', type: 'text', required: true, placeholder: '例如：香薰蜡烛、咖啡机', section: 'basic' },
      { key: 'subject', label: '主体描述', type: 'text', required: true, placeholder: '例如：一只磨砂玻璃香薰杯', section: 'basic' },
      { key: 'scene', label: '使用场景', type: 'text', required: true, placeholder: '例如：清晨木质书桌、浴室边柜', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '自然高级生活方式',
        options: [
          { label: '自然高级生活方式', value: '自然高级生活方式' },
          { label: '杂志广告风', value: '杂志广告风' },
          { label: '北欧静物', value: '北欧静物' }
        ],
        section: 'basic'
      },
      { key: 'sellingPoint', label: '卖点重点', type: 'text', placeholder: '例如：温暖气氛、材质细节、干净台面', section: 'advanced' },
      { key: 'lighting', label: '光线氛围', type: 'text', defaultValue: '柔和窗边自然光', section: 'advanced' }
    ]
  },
  {
    id: 'ui-concept-dashboard',
    title: '科技感 UI 概念图',
    mode: 'text-to-image',
    section: 'common',
    category: 'UI',
    description: '适合生成产品宣传中的界面概念图或未来感面板。',
    tags: ['UI', 'Dashboard', '科技', '界面'],
    previewText: '清晰信息层级、未来感面板、产品概念展示',
    recommendedRatios: ['16:9', '4:3'],
    recommendedModel: 'gpt-image-2',
    badge: '界面',
    promptFragments: [
      '设计一张{product}的 UI 概念图',
      '核心界面为{subject}',
      '整体风格为{style}',
      '主配色采用{color}',
      '重点展示{highlight}',
      '版面结构强调{layout}'
    ],
    fields: [
      { key: 'product', label: '产品类型', type: 'text', required: true, placeholder: '例如：AI 数据分析平台', section: 'basic' },
      { key: 'subject', label: '核心界面', type: 'text', required: true, placeholder: '例如：总览仪表盘、任务面板', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '未来科技感',
        options: [
          { label: '未来科技感', value: '未来科技感' },
          { label: '极简专业 SaaS', value: '极简专业 SaaS' },
          { label: '高密度数据可视化', value: '高密度数据可视化' }
        ],
        section: 'basic'
      },
      { key: 'color', label: '主配色', type: 'text', defaultValue: '青绿与深海军蓝', section: 'advanced' },
      { key: 'highlight', label: '重点信息', type: 'text', placeholder: '例如：图表、告警、增长趋势', section: 'advanced' },
      { key: 'layout', label: '版面结构', type: 'text', defaultValue: '清晰模块化、层级明确', section: 'advanced' }
    ]
  },
  {
    id: 'ui-chat-app-mockup',
    title: '聊天应用界面概念图',
    mode: 'text-to-image',
    section: 'common',
    category: 'UI',
    description: '适合社交、客服、AI 对话类产品的宣传 UI 概念图。',
    tags: ['UI', '聊天', 'App', '对话'],
    previewText: '对话气泡、清晰层级、产品演示感',
    recommendedRatios: ['9:16', '4:3'],
    recommendedModel: 'gpt-image-2',
    badge: '产品',
    promptFragments: [
      '设计一张{product}的移动端聊天界面概念图',
      '核心页面为{subject}',
      '整体风格为{style}',
      '主配色采用{color}',
      '重点表现{highlight}',
      '设备展示形式为{layout}'
    ],
    fields: [
      { key: 'product', label: '产品定位', type: 'text', required: true, placeholder: '例如：AI 学习助手、客服应用', section: 'basic' },
      { key: 'subject', label: '核心页面', type: 'text', required: true, placeholder: '例如：聊天主界面、联系人页', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '极简专业 SaaS',
        options: [
          { label: '极简专业 SaaS', value: '极简专业 SaaS' },
          { label: '明快消费级 App', value: '明快消费级 App' },
          { label: '未来数字界面', value: '未来数字界面' }
        ],
        section: 'basic'
      },
      { key: 'color', label: '主配色', type: 'text', defaultValue: '浅青绿与深灰蓝', section: 'advanced' },
      { key: 'highlight', label: '重点模块', type: 'text', placeholder: '例如：消息气泡、语音入口、快捷卡片', section: 'advanced' },
      { key: 'layout', label: '设备展示', type: 'text', defaultValue: '手机框架居中展示，界面层次清晰', section: 'advanced' }
    ]
  },
  {
    id: 'character-concept-sheet',
    title: '角色设定卡',
    mode: 'text-to-image',
    section: 'common',
    category: '角色',
    description: '适合做角色视觉设定、IP 草图或故事人物卡。',
    tags: ['角色', '设定', 'IP', '人物'],
    previewText: '人物特征清晰、服装完整、设定感强',
    recommendedRatios: ['3:4', '4:5'],
    recommendedModel: 'gpt-image-2',
    badge: 'IP',
    promptFragments: [
      '创作一张{identity}角色设定图',
      '外观特征为{appearance}',
      '服装与配件突出{outfit}',
      '整体风格为{style}',
      '背景设置为{scene}',
      '情绪氛围为{mood}'
    ],
    fields: [
      { key: 'identity', label: '角色身份', type: 'text', required: true, placeholder: '例如：森林邮差少女、赛博侦探', section: 'basic' },
      { key: 'appearance', label: '外观特征', type: 'text', required: true, placeholder: '例如：金色短发、雀斑、圆眼镜', section: 'basic' },
      { key: 'outfit', label: '服装与配件', type: 'text', placeholder: '例如：深绿风衣、旧邮差包', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '插画设定稿',
        options: [
          { label: '插画设定稿', value: '插画设定稿' },
          { label: '动画角色卡', value: '动画角色卡' },
          { label: '电影感角色海报', value: '电影感角色海报' }
        ],
        section: 'basic'
      },
      { key: 'scene', label: '背景', type: 'text', defaultValue: '简洁设定板背景', section: 'advanced' },
      { key: 'mood', label: '情绪氛围', type: 'text', defaultValue: '温和、机灵、有故事感', section: 'advanced' }
    ]
  },
  {
    id: 'storybook-mushroom-house',
    title: '童话插画',
    mode: 'text-to-image',
    section: 'common',
    category: '场景',
    description: '适合做温暖童话、绘本、治愈系世界观设定。',
    tags: ['插画', '童话', '绘本', '治愈'],
    previewText: '柔和光线、故事感场景、童话氛围',
    recommendedRatios: ['3:4', '4:5'],
    recommendedModel: 'gpt-image-2',
    badge: '故事',
    promptFragments: [
      '创作一幅{theme}主题的童话插画',
      '主体是{subject}',
      '场景为{scene}',
      '整体风格为{style}',
      '光线氛围为{lighting}',
      '细节重点表现{details}'
    ],
    fields: [
      { key: 'theme', label: '主题', type: 'text', required: true, placeholder: '例如：黄昏蘑菇屋', section: 'basic' },
      { key: 'subject', label: '主体', type: 'text', required: true, placeholder: '例如：住在蘑菇屋里的小狐狸', section: 'basic' },
      { key: 'scene', label: '场景', type: 'text', defaultValue: '森林边缘的温暖木屋', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '童话绘本',
        options: [
          { label: '童话绘本', value: '童话绘本' },
          { label: '水彩插画', value: '水彩插画' },
          { label: '厚涂油画', value: '厚涂油画' }
        ],
        section: 'basic'
      },
      { key: 'lighting', label: '光线', type: 'text', defaultValue: '黄昏暖光，柔和体积光', section: 'advanced' },
      { key: 'details', label: '细节重点', type: 'text', placeholder: '例如：花草、窗户灯光、石板小路', section: 'advanced' }
    ]
  },
  {
    id: 'cinematic-night-city',
    title: '电影感夜景',
    mode: 'text-to-image',
    section: 'common',
    category: '场景',
    description: '适合赛博朋克、夜雨、霓虹灯等氛围夜景。',
    tags: ['夜景', '电影感', '城市', '摄影', '场景'],
    previewText: '霓虹雨夜、潮湿路面、未来感',
    recommendedRatios: ['16:9', '21:9'],
    recommendedModel: 'gpt-image-2',
    badge: '氛围',
    promptFragments: [
      '创作一幅{scene}的画面',
      '主体是{subject}',
      '整体风格为{style}',
      '光线表现为{lighting}',
      '镜头语言强调{camera}',
      '画面氛围为{mood}'
    ],
    fields: [
      { key: 'scene', label: '场景', type: 'text', required: true, placeholder: '例如：赛博朋克城市雨夜', section: 'basic' },
      { key: 'subject', label: '主体', type: 'text', placeholder: '例如：街头行人、悬浮车辆', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '电影感写实',
        options: [
          { label: '电影感写实', value: '电影感写实' },
          { label: '赛博朋克', value: '赛博朋克' },
          { label: '未来 noir', value: '未来 noir' }
        ],
        section: 'basic'
      },
      { key: 'lighting', label: '光线', type: 'text', defaultValue: '霓虹反射、雨夜湿润路面', section: 'advanced' },
      { key: 'camera', label: '镜头感', type: 'text', placeholder: '例如：广角低机位，空间纵深强', section: 'advanced' },
      { key: 'mood', label: '氛围', type: 'text', defaultValue: '神秘、孤独、未来感', section: 'advanced' }
    ]
  },
  {
    id: 'edit-background-replace',
    title: '保留主体换背景',
    mode: 'image-to-image',
    section: 'common',
    category: '编辑',
    description: '保留参考图主体，只替换背景与环境。',
    tags: ['图生图', '背景替换', '主体保留'],
    previewText: '保留主体、替换背景、统一氛围',
    recommendedRatios: ['1:1', '4:5'],
    requiresReference: true,
    recommendedModel: 'gpt-image-2',
    badge: '常用',
    promptFragments: [
      '保留参考图中的{subject}',
      '将背景替换为{scene}',
      '整体风格调整为{style}',
      '光线氛围改为{lighting}',
      '保持主体比例与主要特征稳定'
    ],
    fields: [
      { key: 'subject', label: '保留主体', type: 'text', required: true, placeholder: '例如：一只金色胖胖大柴犬', section: 'basic' },
      { key: 'scene', label: '目标背景', type: 'text', required: true, placeholder: '例如：温暖办公室、森林木屋', section: 'basic' },
      { key: 'style', label: '目标风格', type: 'text', placeholder: '例如：油画质感、童话插画', section: 'basic' },
      { key: 'lighting', label: '目标光线', type: 'text', placeholder: '例如：黄昏暖光、柔和侧光', section: 'advanced' }
    ]
  },
  {
    id: 'edit-style-transfer',
    title: '改成插画风',
    mode: 'image-to-image',
    section: 'common',
    category: '编辑',
    description: '把参考图转换成指定插画或设计风格。',
    tags: ['图生图', '风格迁移', '插画'],
    previewText: '保持主体轮廓，改造成统一插画风格',
    recommendedRatios: ['1:1', '3:4'],
    requiresReference: true,
    recommendedModel: 'gpt-image-2',
    badge: '风格',
    promptFragments: [
      '以参考图为基础',
      '将整体视觉改造成{style}',
      '保留{preserve}',
      '强化{emphasis}',
      '避免{negative}'
    ],
    fields: [
      {
        key: 'style',
        label: '目标风格',
        type: 'select',
        required: true,
        options: [
          { label: '童话插画', value: '童话插画' },
          { label: '极简几何海报', value: '极简几何海报' },
          { label: '油画质感', value: '油画质感' },
          { label: '扁平品牌插画', value: '扁平品牌插画' }
        ],
        section: 'basic'
      },
      { key: 'preserve', label: '保留内容', type: 'text', defaultValue: '主体轮廓和姿态', section: 'basic' },
      { key: 'emphasis', label: '强化重点', type: 'text', placeholder: '例如：色彩层次、光影氛围', section: 'advanced' },
      { key: 'negative', label: '避免元素', type: 'text', placeholder: '例如：照片感、杂乱细节', section: 'advanced' }
    ]
  },
  {
    id: 'edit-lighting-upgrade',
    title: '增强电影感光影',
    mode: 'image-to-image',
    section: 'common',
    category: '编辑',
    description: '在不大改主体的前提下，强化光影层次和氛围感。',
    tags: ['图生图', '光影', '电影感', '增强'],
    previewText: '保留主体，强化体积光、情绪和层次',
    recommendedRatios: ['16:9', '4:5'],
    requiresReference: true,
    recommendedModel: 'gpt-image-2',
    badge: '氛围',
    promptFragments: [
      '基于参考图保留主体与构图',
      '整体光影调整为{lighting}',
      '氛围改造成{mood}',
      '突出{emphasis}',
      '降低{negative}'
    ],
    fields: [
      { key: 'lighting', label: '目标光线', type: 'text', required: true, placeholder: '例如：电影感侧逆光、空气体积光', section: 'basic' },
      { key: 'mood', label: '目标氛围', type: 'text', required: true, placeholder: '例如：神秘、沉静、潮湿夜色', section: 'basic' },
      { key: 'emphasis', label: '强化重点', type: 'text', placeholder: '例如：人物轮廓、反光、空间深度', section: 'advanced' },
      { key: 'negative', label: '弱化内容', type: 'text', placeholder: '例如：平光、杂乱噪点、普通快照感', section: 'advanced' }
    ]
  },
  {
    id: 'edit-product-retouch',
    title: '商品图精修',
    mode: 'image-to-image',
    section: 'common',
    category: '编辑',
    description: '适合已有商品底图的清洁、质感强化和商业化处理。',
    tags: ['图生图', '商品图', '精修', '电商'],
    previewText: '清理画面、增强材质、统一商业质感',
    recommendedRatios: ['1:1', '4:5'],
    requiresReference: true,
    recommendedModel: 'gpt-image-2',
    badge: '电商',
    promptFragments: [
      '以参考图中的{subject}为主体',
      '整体精修为{style}风格的商业产品图',
      '背景处理为{scene}',
      '重点强化{emphasis}',
      '避免{negative}'
    ],
    fields: [
      { key: 'subject', label: '商品主体', type: 'text', required: true, placeholder: '例如：一瓶磨砂玻璃精华液', section: 'basic' },
      { key: 'style', label: '精修风格', type: 'text', defaultValue: '高级干净', section: 'basic' },
      { key: 'scene', label: '背景处理', type: 'text', defaultValue: '纯净柔和的商业棚拍背景', section: 'basic' },
      { key: 'emphasis', label: '强化重点', type: 'text', placeholder: '例如：玻璃反光、标签细节、液体通透感', section: 'advanced' },
      { key: 'negative', label: '避免元素', type: 'text', placeholder: '例如：脏污、背景褶皱、噪点', section: 'advanced' }
    ]
  },
  {
    id: 'edit-poster-redesign',
    title: '海报重设计',
    mode: 'image-to-image',
    section: 'common',
    category: '编辑',
    description: '适合在已有视觉基础上重做版式、风格和品牌感。',
    tags: ['图生图', '海报', '重设计', '品牌'],
    previewText: '保留关键信息，重做风格和构图节奏',
    recommendedRatios: ['4:5', '3:4'],
    requiresReference: true,
    recommendedModel: 'gpt-image-2',
    badge: '重做',
    promptFragments: [
      '以参考图为基础进行海报重设计',
      '整体风格改为{style}',
      '版式重新组织为{layout}',
      '主配色调整为{color}',
      '强调{emphasis}'
    ],
    fields: [
      { key: 'style', label: '目标风格', type: 'text', required: true, placeholder: '例如：高级时尚、极简现代、复古文艺', section: 'basic' },
      { key: 'layout', label: '版式方向', type: 'text', required: true, placeholder: '例如：居中主视觉、大留白排版', section: 'basic' },
      { key: 'color', label: '主配色', type: 'text', placeholder: '例如：奶油白与深墨绿', section: 'advanced' },
      { key: 'emphasis', label: '重点信息', type: 'text', placeholder: '例如：主体识别、文字区域、品牌质感', section: 'advanced' }
    ]
  },
  {
    id: 'branding-packaging-board',
    title: '品牌包装概念板',
    mode: 'text-to-image',
    section: 'advanced',
    category: '品牌高级',
    description: '适合品牌概念、包装语言和情绪版方向探索，字段更完整。',
    tags: ['品牌', '包装', '品牌板', '高级模板'],
    previewText: '品牌调性、包装材质、主视觉元素统一呈现',
    recommendedRatios: ['4:5', '3:4'],
    recommendedModel: 'gpt-image-2',
    badge: '进阶',
    promptFragments: [
      '为{brand}设计一张品牌包装概念板',
      '产品品类为{product}',
      '品牌调性为{tone}',
      '主视觉元素包含{visuals}',
      '主配色采用{color}',
      '材质与包装结构强调{material}',
      '文字区与信息层级偏向{layout}'
    ],
    fields: [
      { key: 'brand', label: '品牌名称 / 项目名', type: 'text', required: true, placeholder: '例如：LUMA 护肤系列', section: 'basic' },
      { key: 'product', label: '产品品类', type: 'text', required: true, placeholder: '例如：精华液、气泡水、香氛礼盒', section: 'basic' },
      { key: 'tone', label: '品牌调性', type: 'text', required: true, placeholder: '例如：洁净、未来、轻奢、年轻', section: 'basic' },
      { key: 'visuals', label: '主视觉元素', type: 'text', placeholder: '例如：流线波纹、几何花瓣、玻璃反射', section: 'basic' },
      { key: 'color', label: '主配色', type: 'text', defaultValue: '奶白、银灰与薄荷青', section: 'advanced' },
      { key: 'material', label: '材质与结构', type: 'text', placeholder: '例如：磨砂玻璃瓶、金属泵头、极简外盒', section: 'advanced' },
      { key: 'layout', label: '信息层级', type: 'text', placeholder: '例如：大品牌名、小规格信息、留白布局', section: 'advanced' }
    ]
  },
  {
    id: 'branding-mascot-board',
    title: '品牌吉祥物延展板',
    mode: 'text-to-image',
    section: 'advanced',
    category: '品牌高级',
    description: '适合把品牌人格、吉祥物和应用场景一起整理成概念板。',
    tags: ['品牌', '吉祥物', '概念板', '高级模板'],
    previewText: 'IP 角色、品牌气质、应用场景一体化呈现',
    recommendedRatios: ['4:5', '16:9'],
    recommendedModel: 'gpt-image-2',
    badge: '高级',
    promptFragments: [
      '为{brand}设计一张品牌吉祥物延展板',
      '吉祥物设定为{subject}',
      '品牌调性为{tone}',
      '应用场景包括{scene}',
      '整体风格为{style}',
      '版面组织突出{layout}'
    ],
    fields: [
      { key: 'brand', label: '品牌名称', type: 'text', required: true, placeholder: '例如：MOMO 茶饮', section: 'basic' },
      { key: 'subject', label: '吉祥物设定', type: 'text', required: true, placeholder: '例如：软萌猫咪店长，戴围裙', section: 'basic' },
      { key: 'tone', label: '品牌调性', type: 'text', placeholder: '例如：活泼、温暖、年轻社交', section: 'basic' },
      { key: 'scene', label: '应用场景', type: 'text', placeholder: '例如：门店海报、杯套、贴纸、社媒头像', section: 'advanced' },
      { key: 'style', label: '视觉风格', type: 'text', placeholder: '例如：扁平插画、复古漫画、奶油质感', section: 'advanced' },
      { key: 'layout', label: '版面重点', type: 'text', placeholder: '例如：角色主视觉居中，周围散布周边应用', section: 'advanced' }
    ]
  },
  {
    id: 'infographic-process-board',
    title: '流程信息图',
    mode: 'text-to-image',
    section: 'advanced',
    category: '信息图高级',
    description: '适合做步骤说明、流程图和教学型信息图。',
    tags: ['信息图', '流程', '说明图', '高级模板'],
    previewText: '步骤清晰、层级明确、图标和文字并重',
    recommendedRatios: ['4:5', '16:9'],
    recommendedModel: 'gpt-image-2',
    badge: '进阶',
    promptFragments: [
      '设计一张关于{topic}的流程信息图',
      '主要步骤包括{steps}',
      '信息结构采用{structure}',
      '视觉风格为{style}',
      '图标与示意元素突出{visuals}',
      '配色以{color}为主'
    ],
    fields: [
      { key: 'topic', label: '主题', type: 'text', required: true, placeholder: '例如：新员工入职流程、咖啡制作步骤', section: 'basic' },
      { key: 'steps', label: '核心步骤', type: 'textarea', required: true, placeholder: '例如：注册账号 / 验证邮箱 / 完成培训 / 开始使用', section: 'basic' },
      { key: 'structure', label: '结构形式', type: 'text', defaultValue: '纵向步骤卡片 + 连线箭头', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '现代清晰信息图',
        options: [
          { label: '现代清晰信息图', value: '现代清晰信息图' },
          { label: '教育海报风', value: '教育海报风' },
          { label: '科技说明图', value: '科技说明图' }
        ],
        section: 'basic'
      },
      { key: 'visuals', label: '图示元素', type: 'text', placeholder: '例如：线性图标、编号圆点、轻量插图', section: 'advanced' },
      { key: 'color', label: '配色', type: 'text', defaultValue: '青绿、海军蓝和浅灰', section: 'advanced' }
    ]
  },
  {
    id: 'slide-visual-summary',
    title: '单页视觉报告',
    mode: 'text-to-image',
    section: 'advanced',
    category: '信息图高级',
    description: '适合政策说明、教育信息、单页汇总类视觉文档。',
    tags: ['视觉报告', '信息图', '单页', '高级模板'],
    previewText: '信息分块、重点突出、适合展示型报告',
    recommendedRatios: ['16:9', '4:3'],
    recommendedModel: 'gpt-image-2',
    badge: '高级',
    promptFragments: [
      '设计一张关于{topic}的单页视觉报告',
      '主要信息块包括{sections}',
      '整体风格为{style}',
      '重点指标突出{highlight}',
      '版面结构强调{layout}',
      '配色采用{color}'
    ],
    fields: [
      { key: 'topic', label: '主题', type: 'text', required: true, placeholder: '例如：AI 趋势摘要、年度运营回顾', section: 'basic' },
      { key: 'sections', label: '主要信息块', type: 'textarea', required: true, placeholder: '例如：背景 / 核心数据 / 结论 / 建议', section: 'basic' },
      { key: 'style', label: '风格', type: 'text', defaultValue: '专业清晰、可展示', section: 'basic' },
      { key: 'highlight', label: '重点指标', type: 'text', placeholder: '例如：增长率、关键对比、三条结论', section: 'advanced' },
      { key: 'layout', label: '版面结构', type: 'text', defaultValue: '模块卡片 + 主标题 + 重点数字', section: 'advanced' },
      { key: 'color', label: '配色', type: 'text', defaultValue: '白底、青绿强调、深灰标题', section: 'advanced' }
    ]
  },
  {
    id: 'avatar-profile-series',
    title: '社媒头像系列',
    mode: 'text-to-image',
    section: 'advanced',
    category: '头像与封面',
    description: '适合创作者头像、频道封面和风格统一的社媒视觉。',
    tags: ['头像', '社媒', '封面', '高级模板'],
    previewText: '人物或角色聚焦、裁切明确、平台化展示',
    recommendedRatios: ['1:1', '16:9'],
    recommendedModel: 'gpt-image-2',
    badge: '进阶',
    promptFragments: [
      '为{subject}设计一套社媒头像与封面视觉',
      '整体风格为{style}',
      '背景设置为{scene}',
      '主配色为{color}',
      '裁切重点突出{layout}',
      '氛围表现为{mood}'
    ],
    fields: [
      { key: 'subject', label: '主体', type: 'text', required: true, placeholder: '例如：个人创作者、虚拟角色、播客主持人', section: 'basic' },
      {
        key: 'style',
        label: '风格',
        type: 'select',
        defaultValue: '干净个人品牌',
        options: [
          { label: '干净个人品牌', value: '干净个人品牌' },
          { label: '高饱和插画头像', value: '高饱和插画头像' },
          { label: '未来感频道封面', value: '未来感频道封面' }
        ],
        section: 'basic'
      },
      { key: 'scene', label: '背景', type: 'text', placeholder: '例如：纯色渐层、城市霓虹、抽象图形', section: 'basic' },
      { key: 'color', label: '主配色', type: 'text', defaultValue: '青绿、深蓝与白', section: 'advanced' },
      { key: 'layout', label: '裁切重点', type: 'text', placeholder: '例如：头像近景、封面横向构图、左右留文案位', section: 'advanced' },
      { key: 'mood', label: '氛围', type: 'text', placeholder: '例如：亲和、专业、先锋', section: 'advanced' }
    ]
  },
  {
    id: 'avatar-sticker-pack',
    title: '角色贴纸头像包',
    mode: 'text-to-image',
    section: 'advanced',
    category: '头像与封面',
    description: '适合把角色做成贴纸化头像组或社媒表情包视觉。',
    tags: ['头像', '贴纸', '角色', '高级模板'],
    previewText: '统一角色风格、多表情延展、头像化裁切',
    recommendedRatios: ['1:1'],
    recommendedModel: 'gpt-image-2',
    badge: '高级',
    promptFragments: [
      '为{subject}设计一组角色贴纸头像包',
      '整体风格为{style}',
      '重点表情包括{expressions}',
      '配色采用{color}',
      '背景处理为{scene}'
    ],
    fields: [
      { key: 'subject', label: '角色主体', type: 'text', required: true, placeholder: '例如：戴耳机的小狐狸主播', section: 'basic' },
      { key: 'style', label: '风格', type: 'text', defaultValue: '扁平可爱贴纸插画', section: 'basic' },
      { key: 'expressions', label: '表情方向', type: 'text', placeholder: '例如：开心、害羞、震惊、点赞', section: 'basic' },
      { key: 'color', label: '配色', type: 'text', defaultValue: '高饱和糖果色', section: 'advanced' },
      { key: 'scene', label: '背景处理', type: 'text', placeholder: '例如：纯色背景、透明边框感、轻描边', section: 'advanced' }
    ]
  }
]
