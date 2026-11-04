export const DESIGNER_PROMPT_TAGS = ['电商主图', '电商详情图', '电商白底图', '电商 SKU 图'] as const

export interface DesignerPromptTemplate {
  id: string
  title: string
  tags: readonly (typeof DESIGNER_PROMPT_TAGS[number])[]
  image: string
  prompt: string
}

export const DESIGNER_PROMPTS: readonly DesignerPromptTemplate[] = [
  {
    id: 'hero-studio',
    title: '极简留白主图',
    tags: ['电商主图'],
    image: '/designer-prompts/hero-studio.webp',
    prompt: '为【商品名称】制作一张极简电商主图。以参考图中的商品为主体，保留真实外形、材质、包装和品牌标识。商品居中置于浅色石质展台，占画面约三分之二；暖灰背景，大面积留白，柔和侧光与自然接触阴影。突出【核心卖点】对应的产品细节。画面干净，无多余道具，不添加未经提供的文字、价格或功效宣称。',
  },
  {
    id: 'hero-natural',
    title: '自然光场景主图',
    tags: ['电商主图'],
    image: '/designer-prompts/hero-natural.webp',
    prompt: '为【商品名称】制作自然光风格的电商主图。保持参考商品的比例、包装、颜色与标识一致，置于木质圆形展台。米白背景上有柔和的植物光影，暖色阳光从侧面照入，突出商品轮廓和【材质特点】。商品清晰醒目，道具克制，背景不抢主体。保留适量排版留白，不生成额外文字或虚构产品信息。',
  },
  {
    id: 'detail-liquid',
    title: '产品细节特写',
    tags: ['电商详情图'],
    image: '/designer-prompts/detail-liquid.webp',
    prompt: '为【商品名称】制作一张电商详情特写图，重点呈现【需要展示的细节】。依据参考图准确还原结构、材质与颜色，使用微距产品摄影构图，焦点集中于关键部位，背景简洁、景深柔和，用侧逆光表现真实纹理与通透感。保留一侧留白供后期排版。仅展示已提供的产品细节，不虚构成分、效果或认证，不添加文字。',
  },
  {
    id: 'detail-texture',
    title: '材质与卖点详情',
    tags: ['电商详情图'],
    image: '/designer-prompts/detail-texture.webp',
    prompt: '为【商品名称】制作材质展示详情图。以【真实材质或质地】为视觉中心，近距离呈现细腻纹理、自然起伏和光泽，参考图中的颜色与质感必须保持一致。采用低饱和纯色背景、柔和侧光，主体集中在画面中下部，上方留出标题区域。通过视觉呈现【已确认的核心卖点】，不虚构数据、功效、成分，不生成文字。',
  },
  {
    id: 'white-clean',
    title: '纯白商品图',
    tags: ['电商白底图'],
    image: '/designer-prompts/white-clean.webp',
    prompt: '将参考图中的【商品名称】制作成标准电商白底商品图。准确保留商品外形、比例、颜色、包装和可见标识，完整展示商品，四周保留均匀安全边距。背景为纯白色，灯光均匀，边缘清晰，仅保留极轻的接触阴影。移除环境与道具，不裁切商品，不增加装饰、文字、水印或未提供的配件。',
  },
  {
    id: 'white-soft',
    title: '柔光白底图',
    tags: ['电商白底图'],
    image: '/designer-prompts/white-soft.webp',
    prompt: '为【商品名称】制作柔光白底商品图。依据参考图还原真实结构、材质、配色和品牌标识，以略微俯视的三分之四角度完整展示商品。使用白色无缝背景和大面积柔光，保留自然、浅淡的落地阴影，清晰呈现金属、玻璃或包装的真实质感。主体居中且不贴边，无场景道具、文字、水印和夸张反光。',
  },
  {
    id: 'sku-variants',
    title: '多规格对比',
    tags: ['电商 SKU 图'],
    image: '/designer-prompts/sku-variants.webp',
    prompt: '为【商品名称】制作电商 SKU 规格对比图。按照【规格清单】中的真实款式、容量或颜色分别展示商品，严格参考对应图片，不自行补充型号。所有商品使用一致的拍摄角度、光线和背景，按实际尺寸关系排列，间距均匀、边缘完整，清晰区分各个选项。采用白色背景和轻微阴影，底部预留标注位置，不自动生成规格文字或价格。',
  },
  {
    id: 'sku-set',
    title: '套装组合展示',
    tags: ['电商 SKU 图'],
    image: '/designer-prompts/sku-set.webp',
    prompt: '为【商品名称】制作套装 SKU 展示图。仅展示【套装清单】列出的商品与数量，保持每件商品与参考图的外观、包装、颜色及比例一致。采用俯拍平铺构图，主商品占据视觉中心，配件有序排布，统一柔光与浅色背景，所有物件完整入镜且互不遮挡。清晰表达套装包含内容，不增加赠品、价格、促销标签或未提供的文字。',
  },
]
