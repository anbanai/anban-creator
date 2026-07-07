<template>
  <view class="credits-page">
    <!-- Loading -->
    <AbLoading v-if="pageLoading" text="加载中..." />

    <view v-else>
      <!-- Balance Hero Card -->
      <view class="hero">
        <view class="hero__top">
          <text class="hero__label">积分余额</text>
          <view class="hero__tier">
            <AbBadge :variant="tierBadgeVariant" size="sm">{{ tierLabel }}</AbBadge>
          </view>
        </view>
        <view class="hero__balance">
          <text class="hero__value">{{ displayBalance }}</text>
          <text class="hero__unit">积分</text>
        </view>
        <view class="hero__actions">
          <view
            class="hero__signin"
            :class="{ 'hero__signin--done': signedInToday }"
            @tap="handleSignIn"
          >
            <text v-if="signInLoading" class="hero__signin-spinner" />
            <text class="hero__signin-text">
              {{ signedInToday ? '✓ 今日已签到' : `签到 +${dailySignInCredits}` }}
            </text>
          </view>
          <view class="hero__recharge" @tap="goRecharge">
            <text class="hero__recharge-icon">卡</text>
            <text class="hero__recharge-text">充值</text>
          </view>
        </view>
      </view>

      <!-- Membership Tiers -->
      <view class="section">
        <view class="section__header">
          <text class="section__title">会员等级</text>
          <text class="section__hint">不同等级享受不同权益</text>
        </view>
        <scroll-view scroll-x class="tiers" :show-scrollbar="false">
          <view class="tiers__track">
            <view
              v-for="tier in tierBenefits"
              :key="tier.key"
              class="tier-card"
              :class="{
                'tier-card--current': tier.key === currentTier,
                'tier-card--recommended': tier.recommended,
              }"
            >
              <view class="tier-card__head">
                <text class="tier-card__name">{{ tier.name }}</text>
                <view v-if="tier.recommended" class="tier-card__rec">
                  <text class="tier-card__rec-text">推荐</text>
                </view>
              </view>
              <text class="tier-card__desc">{{ tier.description }}</text>
              <view class="tier-card__row">
                <text class="tier-card__row-label">积分倍率</text>
                <text class="tier-card__row-value">{{ tier.creditMultiplier }}</text>
              </view>
              <view class="tier-card__row">
                <text class="tier-card__row-label">并发任务</text>
                <text class="tier-card__row-value">{{ tier.concurrentTasks }} 个</text>
              </view>
              <view class="tier-card__platforms">
                <text
                  v-for="p in tier.platforms"
                  :key="p"
                  class="tier-card__platform"
                >{{ p }}</text>
              </view>
              <view v-if="tier.key === currentTier" class="tier-card__current-badge">
                <text class="tier-card__current-badge-text">当前</text>
              </view>
            </view>
          </view>
        </scroll-view>
      </view>

      <!-- Model Capabilities -->
      <view class="section">
        <view class="section__header">
          <text class="section__title">模型能力</text>
          <text class="section__hint">不同会员等级可使用的模型类别</text>
        </view>
        <AbCard :padding="0">
          <view class="model-grid">
            <view
              v-for="tier in tierBenefits"
              :key="tier.key"
              class="model-col"
              :class="{ 'model-col--current': tier.key === currentTier }"
            >
              <text class="model-col__name">{{ tier.name }}</text>
              <view
                v-for="cat in modelCategories"
                :key="cat.key"
                class="model-row"
              >
                <text class="model-row__icon">{{ cat.available[tier.key] ? '✓' : '–' }}</text>
                <text
                  class="model-row__label"
                  :class="{ 'model-row__label--on': cat.available[tier.key] }"
                >{{ cat.label }}</text>
              </view>
            </view>
          </view>
        </AbCard>
        <view class="model-legend">
          <text
            v-for="cat in modelCategories"
            :key="cat.key"
            class="model-legend__item"
          >
            <text class="model-legend__name">{{ cat.label }}</text>
            <text class="model-legend__dash"> — </text>
            <text class="model-legend__desc">{{ cat.description }}</text>
          </text>
        </view>
      </view>

      <!-- Pricing Guide (Accordion) -->
      <view v-if="pricing" class="section">
        <view class="section__header">
          <text class="section__title">计费说明</text>
        </view>
        <AbCard :padding="0">
          <!-- Task costs -->
          <view class="accordion-item">
            <view class="accordion-head" @tap="toggleSection('task')">
              <text class="accordion-head__title">任务基础服务费</text>
              <text class="accordion-head__arrow" :class="{ 'accordion-head__arrow--open': openSections.task }">›</text>
            </view>
            <view v-if="openSections.task" class="accordion-body">
              <view
                v-for="row in taskCostRows"
                :key="row.label"
                class="price-row"
                :class="{ 'price-row--alt': row.alt }"
              >
                <text class="price-row__label">{{ row.label }}</text>
                <text class="price-row__value">{{ row.value }}</text>
              </view>
              <text class="accordion-footnote">图片、视频、写作模型等 MCP 操作按实际用量另计，最终以交易明细汇总为准。</text>
            </view>
          </view>

          <!-- E-commerce module estimates -->
          <view v-if="ecommerceRows.length > 0" class="accordion-item">
            <view class="accordion-head" @tap="toggleSection('ecommerce')">
              <text class="accordion-head__title">电商素材模块（交付规模参考）</text>
              <text class="accordion-head__arrow" :class="{ 'accordion-head__arrow--open': openSections.ecommerce }">›</text>
            </view>
            <view v-if="openSections.ecommerce" class="accordion-body">
              <view
                v-for="row in ecommerceRows"
                :key="row.label"
                class="price-row"
                :class="{ 'price-row--alt': row.alt }"
              >
                <text class="price-row__label">{{ row.label }}</text>
                <text class="price-row__value">{{ row.value }}</text>
              </view>
              <text class="accordion-footnote">创建电商任务只扣基础服务费；模块数量用于估算后续图片生成和理解操作规模，最终以交易明细汇总为准。</text>
            </view>
          </view>

          <!-- Image model costs -->
          <view v-if="imageModelRows.length > 0" class="accordion-item">
            <view class="accordion-head" @tap="toggleSection('image')">
              <text class="accordion-head__title">图片生成（使用平台模型按次扣费）</text>
              <text class="accordion-head__arrow" :class="{ 'accordion-head__arrow--open': openSections.image }">›</text>
            </view>
            <view v-if="openSections.image" class="accordion-body">
              <view
                v-for="row in imageModelRows"
                :key="row.label"
                class="price-row"
                :class="{ 'price-row--alt': row.alt }"
              >
                <text class="price-row__label">{{ row.label }}</text>
                <text class="price-row__value">{{ row.value }}</text>
              </view>
            </view>
          </view>

          <!-- Text operation costs -->
          <view v-if="textGroups.length > 0" class="accordion-item">
            <view class="accordion-head" @tap="toggleSection('text')">
              <text class="accordion-head__title">文本操作（使用平台模型按次扣费）</text>
              <text class="accordion-head__arrow" :class="{ 'accordion-head__arrow--open': openSections.text }">›</text>
            </view>
            <view v-if="openSections.text" class="accordion-body">
              <view
                v-for="group in textGroups"
                :key="group.model"
                class="text-group"
              >
                <text class="text-group__model">{{ group.model }}</text>
                <view
                  v-for="row in group.rows"
                  :key="row.label"
                  class="price-row"
                  :class="{ 'price-row--alt': row.alt }"
                >
                  <text class="price-row__label">{{ row.label }}</text>
                  <text class="price-row__value">{{ row.value }}</text>
                </view>
              </view>
            </view>
          </view>

          <!-- Income & free items -->
          <view class="accordion-item accordion-item--last">
            <view class="accordion-head" @tap="toggleSection('income')">
              <text class="accordion-head__title">积分获取 & 免费项目</text>
              <text class="accordion-head__arrow" :class="{ 'accordion-head__arrow--open': openSections.income }">›</text>
            </view>
            <view v-if="openSections.income" class="accordion-body">
              <view
                v-for="row in incomeRows"
                :key="row.label"
                class="price-row"
                :class="{ 'price-row--alt': row.alt }"
              >
                <text class="price-row__label">{{ row.label }}</text>
                <text class="price-row__value">{{ row.value }}</text>
              </view>
              <view class="free-box">
                <text class="free-box__line">图片上传、草稿发布 → 免费</text>
                <text class="free-box__line">使用自己的模型（BYOK）→ 全部免费</text>
              </view>
            </view>
          </view>
        </AbCard>
      </view>

      <!-- Recent Transactions Preview -->
      <view class="section">
        <view class="section__header">
          <text class="section__title">最近交易</text>
          <text
            v-if="recentTransactions.length > 0"
            class="section__link"
            @tap="goAllTransactions"
          >全部 ›</text>
        </view>
        <AbEmpty
          v-if="recentTransactions.length === 0"
          title="暂无交易记录"
          description="完成签到或创建任务即可获得积分"
        />
        <AbCard v-else :padding="0">
          <view
            v-for="(item, idx) in recentTransactions"
            :key="item.id"
            class="tx-item"
            :class="{ 'tx-item--last': idx === recentTransactions.length - 1 }"
          >
            <view class="tx-item__left">
              <view class="tx-item__badge" :class="txBadgeClass(item.type)">
                <text class="tx-item__badge-icon">{{ txIcon(item.type) }}</text>
              </view>
              <view class="tx-item__detail">
                <text class="tx-item__type">{{ txTypeLabel(item.type) }}</text>
                <text class="tx-item__desc">{{ item.description || '—' }}</text>
              </view>
            </view>
            <view class="tx-item__right">
              <text
                class="tx-item__amount"
                :class="{
                  'tx-item__amount--income': item.amount > 0,
                  'tx-item__amount--expense': item.amount < 0,
                }"
              >
                {{ item.amount > 0 ? '+' : '' }}{{ item.amount.toLocaleString() }}
              </text>
              <text class="tx-item__time">{{ relativeTime(item.created_at) }}</text>
            </view>
          </view>
          <view class="tx-more" @tap="goAllTransactions">
            <text class="tx-more__text">查看全部交易记录 ›</text>
          </view>
        </AbCard>
      </view>

      <!-- Bottom spacer -->
      <view class="bottom-spacer" />
    </view>

    <!-- Recharge Popup -->
    <view v-if="showRecharge" class="popup-mask" @tap="showRecharge = false">
      <view class="popup-sheet" @tap.stop>
        <view class="popup-sheet__header">
          <text class="popup-sheet__title">充值积分</text>
          <text class="popup-sheet__close" @tap="showRecharge = false">✕</text>
        </view>
        <view class="recharge-tiers">
          <view
            v-for="tier in rechargeTiers"
            :key="tier.key"
            class="recharge-tier"
            :class="{ 'recharge-tier--active': selectedTier === tier.key }"
            @tap="selectedTier = tier.key"
          >
            <text class="recharge-tier__price">{{ tier.price }} 元</text>
            <text class="recharge-tier__credits">{{ tier.credits.toLocaleString() }} 积分</text>
            <text v-if="tier.bonus" class="recharge-tier__bonus">多送{{ tier.bonus.toLocaleString() }}</text>
          </view>
        </view>
        <view class="recharge-divider">
          <view class="recharge-divider__line" />
          <text class="recharge-divider__text">或扫码联系客服充值</text>
          <view class="recharge-divider__line" />
        </view>
        <view class="recharge-qr">
          <view class="recharge-qr__placeholder">
            <text class="recharge-qr__icon">扫</text>
            <text class="recharge-qr__label">客服二维码</text>
            <text class="recharge-qr__hint">长按识别添加客服</text>
          </view>
        </view>
        <view class="recharge-footer">
          <AbButton type="primary" size="lg" block :disabled="!selectedTier" @click="confirmRecharge">
            确认充值
          </AbButton>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted } from 'vue'
import { onPullDownRefresh, onShow } from '@dcloudio/uni-app'
import { creditsApi } from '@/api/credits'
import { useAuthStore } from '@/stores/auth'
import {
  tierLabels,
  transactionTypeLabel,
  taskTypeLabelCN,
  operationLabel,
  ecommerceModuleCatalog,
} from '@/utils/labels'
import { relativeTime } from '@/utils/format'
import AbLoading from '@/components/common/AbLoading.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbCard from '@/components/common/AbCard.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import AbButton from '@/components/common/AbButton.vue'
import type {
  CreditBalance,
  SignInStatus,
  CreditTransaction,
  CreditTransactionType,
  CreditPricing,
} from '@/types'

// --- Static catalog (mirror studio labels) ---
const tierBenefits = [
  {
    key: 'free',
    name: '免费版',
    description: '适合轻量体验',
    creditMultiplier: '1.0x',
    platforms: ['Web 端'],
    concurrentTasks: 2,
    recommended: false,
  },
  {
    key: 'pro',
    name: '专业版',
    description: '适合稳定创作',
    creditMultiplier: '1.2x',
    platforms: ['Web 端', 'Claude Code', 'OpenClaw', 'Codex'],
    concurrentTasks: 5,
    recommended: true,
  },
  {
    key: 'enterprise',
    name: '企业版',
    description: '适合团队和深度运营',
    creditMultiplier: '1.5x',
    platforms: ['全部平台', '业务指导'],
    concurrentTasks: 10,
    recommended: false,
  },
] as const

type TierKey = 'free' | 'pro' | 'enterprise'

interface ModelCategory {
  key: string
  label: string
  description: string
  available: Record<TierKey, boolean>
}

const modelCategories: ModelCategory[] = [
  {
    key: 'basic',
    label: '基础模型',
    description: '日常任务的高性价比选择',
    available: { free: true, pro: true, enterprise: true },
  },
  {
    key: 'advanced',
    label: '高级模型',
    description: '顶级模型，长文与复杂任务质量更佳',
    available: { free: false, pro: false, enterprise: true },
  },
  {
    key: 'custom',
    label: '自定义模型 (BYOK)',
    description: '绑定自己的 API Key，全部操作免费',
    available: { free: false, pro: true, enterprise: true },
  },
]

// --- Auth / tier ---
const authStore = useAuthStore()
const currentTier = computed<TierKey>(
  () => (authStore.user?.tier as TierKey) || 'free',
)

const tierLabel = computed(
  () => tierLabels[authStore.user?.tier || 'free'] || '免费版',
)

const tierBadgeVariant = computed<'info' | 'warning' | 'neutral'>(() => {
  switch (authStore.user?.tier) {
    case 'pro': return 'info'
    case 'enterprise': return 'warning'
    default: return 'neutral'
  }
})

// --- Data ---
const balance = ref(0)
const signedInToday = ref(false)
const pricing = ref<CreditPricing | null>(null)
const recentTransactions = ref<CreditTransaction[]>([])
const pageLoading = ref(true)
const signInLoading = ref(false)

const displayBalance = computed(() => balance.value.toLocaleString())

const fallbackIncome = {
  daily_sign_in: 100,
  register_bonus: 1000,
  invite_reward: 1000,
}

const dailySignInCredits = computed(
  () => pricing.value?.income?.daily_sign_in ?? fallbackIncome.daily_sign_in,
)

async function loadBalance() {
  try {
    const res: CreditBalance = await creditsApi.balance()
    balance.value = res.balance ?? 0
  } catch {
    // silent
  }
}

async function loadSignInStatus() {
  try {
    const res: SignInStatus = await creditsApi.signInStatus()
    signedInToday.value = !!res.signed_in_today
  } catch {
    // silent
  }
}

async function loadPricing() {
  try {
    pricing.value = await creditsApi.pricing()
  } catch {
    // silent
  }
}

async function loadRecentTransactions() {
  try {
    const res = await creditsApi.transactions({ limit: 5, offset: 0 })
    recentTransactions.value = res.items || []
  } catch {
    // silent
  }
}

async function loadAll() {
  pageLoading.value = true
  try {
    await Promise.all([
      loadBalance(),
      loadSignInStatus(),
      loadPricing(),
      loadRecentTransactions(),
    ])
  } finally {
    pageLoading.value = false
  }
}

onMounted(loadAll)
// Refresh on tab re-entry (balance may change after a task runs)
onShow(() => {
  if (!pageLoading.value) {
    loadBalance()
    loadSignInStatus()
  }
})

// --- Pull-to-refresh ---
onPullDownRefresh(async () => {
  try {
    await loadAll()
  } finally {
    uni.stopPullDownRefresh()
  }
})

// --- Sign-in ---
async function handleSignIn() {
  if (signedInToday.value || signInLoading.value) return
  signInLoading.value = true
  try {
    await creditsApi.signIn()
    signedInToday.value = true
    uni.showToast({
      title: `签到成功 +${dailySignInCredits.value}`,
      icon: 'success',
    })
    await Promise.all([loadBalance(), loadRecentTransactions()])
  } catch (err: any) {
    const msg = err?.message || '签到失败，请重试'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    signInLoading.value = false
  }
}

// --- Pricing rows ---
interface PriceRow {
  label: string
  value: string
  alt: boolean
}

function withAlt(rows: { label: string; value: string }[]): PriceRow[] {
  return rows.map((r, i) => ({ ...r, alt: i % 2 === 0 }))
}

const taskCostRows = computed<PriceRow[]>(() => {
  if (!pricing.value) return []
  return withAlt(
    Object.entries(pricing.value.task_costs).map(([type, cost]) => ({
      label: taskTypeLabelCN[type] ?? type,
      value: `${(cost ?? 0).toLocaleString()} 积分`,
    })),
  )
})

const ecommerceRows = computed<PriceRow[]>(() => {
  if (!pricing.value?.ecommerce_module_prices) return []
  const prices = pricing.value.ecommerce_module_prices
  return withAlt(
    ecommerceModuleCatalog
      .map((mod) => {
        const price = prices[mod.key]
        if (price == null) return null
        return {
          label: `${mod.label}（${mod.ratio}）`,
          value: `${price} 积分/${mod.qtyLabel}`,
        }
      })
      .filter((r): r is { label: string; value: string } => r !== null),
  )
})

const imageModelRows = computed<PriceRow[]>(() => {
  if (!pricing.value) return []
  const imageModels = pricing.value.model_costs.image_gen ?? {}
  return withAlt(
    Object.entries(imageModels).map(([model, cost]) => ({
      label: model,
      value: `${cost} 积分/张`,
    })),
  )
})

interface TextGroup {
  model: string
  rows: PriceRow[]
}

const textGroups = computed<TextGroup[]>(() => {
  if (!pricing.value) return []
  const textOps = Object.entries(pricing.value.model_costs).filter(
    ([op]) => op !== 'image_gen',
  )
  if (textOps.length === 0) return []
  const textModels = [...new Set(textOps.flatMap(([, m]) => Object.keys(m)))]
  return textModels.map((model) => ({
    model,
    rows: withAlt(
      textOps
        .filter(([, models]) => models[model] !== undefined)
        .map(([op, models]) => ({
          label: operationLabel[op] ?? op,
          value: `${models[model]} 积分`,
        })),
    ),
  }))
})

const incomeRows = computed<PriceRow[]>(() => {
  if (!pricing.value) return []
  const inc = { ...fallbackIncome, ...pricing.value.income }
  return withAlt([
    { label: '每日签到', value: `+${(inc.daily_sign_in ?? 0).toLocaleString()}` },
    { label: '注册奖励', value: `+${(inc.register_bonus ?? 0).toLocaleString()}` },
    { label: '邀请奖励', value: `+${(inc.invite_reward ?? 0).toLocaleString()}` },
  ])
})

// --- Accordion state ---
const openSections = reactive({
  task: true,
  ecommerce: false,
  image: false,
  text: false,
  income: false,
})

function toggleSection(key: keyof typeof openSections) {
  openSections[key] = !openSections[key]
}

// --- Transaction helpers ---
const incomeTypes: CreditTransactionType[] = ['sign_in', 'task_refund', 'admin_grant', 'register_bonus', 'invite_reward']

function txTypeLabel(type: CreditTransactionType): string {
  return transactionTypeLabel[type] || type
}

function txIcon(type: CreditTransactionType): string {
  switch (type) {
    case 'sign_in': return '签'
    case 'task_deduct': return '任'
    case 'task_refund': return '返'
    case 'admin_grant': return '赠'
    case 'register_bonus': return '注'
    case 'invite_reward': return '邀'
    case 'image_gen': return '图'
    case 'image_understanding': return '识'
    case 'image_upload': return '传'
    case 'article_write': return '文'
    case 'convert': return '转'
    case 'humanize': return '润'
    case 'topic_research': return '搜'
    case 'seo': return '势'
    case 'draft_publish': return '发'
    case 'outline': return '纲'
    case 'viral_analysis': return '析'
    case 'video_gen': return '视'
    case 'video_understanding': return '理'
    case 'poster_generation': return '海'
    default: return '分'
  }
}

function txBadgeClass(type: CreditTransactionType): string {
  return incomeTypes.includes(type)
    ? 'tx-item__badge--income'
    : 'tx-item__badge--expense'
}

// --- Navigation ---
function goAllTransactions() {
  uni.navigateTo({ url: '/pages/credits/transactions' })
}

// --- Recharge popup ---
const showRecharge = ref(false)
const selectedTier = ref<string | null>(null)

interface RechargeTier {
  key: string
  label: string
  price: number
  credits: number
  bonus?: number
}

const fallbackRechargeTiers: RechargeTier[] = [
  { key: 'basic', label: '基础包', price: 10, credits: 10000 },
  { key: 'standard', label: '标准包', price: 50, credits: 52000, bonus: 2000 },
  { key: 'pro', label: '进阶包', price: 100, credits: 110000, bonus: 10000 },
]

const rechargeTiers = computed<RechargeTier[]>(() => {
  if (pricing.value?.recharge_tiers == null) {
    return fallbackRechargeTiers
  }
  return pricing.value.recharge_tiers
    ?.filter((tier) => tier.enabled !== false && tier.price_cny > 0 && tier.credits > 0)
    .map((tier) => ({
      key: tier.key,
      label: tier.label,
      price: tier.price_cny,
      credits: tier.credits,
      bonus: tier.bonus_credits,
    })) ?? []
})

function goRecharge() {
  selectedTier.value = null
  showRecharge.value = true
}

function confirmRecharge() {
  if (!selectedTier.value) return
  // No real payment endpoint on mini-app yet — payment is via customer service QR.
  uni.showToast({ title: '请扫码联系客服完成充值', icon: 'none' })
}
</script>

<style lang="scss" scoped>
.credits-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  box-sizing: border-box;
}

// --- Hero card ---
.hero {
  background: linear-gradient(135deg, $ab-primary 0%, $ab-primary-light 100%);
  border-radius: $ab-radius-lg;
  padding: $ab-space-lg $ab-space-md;
  margin-bottom: $ab-space-md;
  box-shadow: $ab-shadow-md;

  &__top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: $ab-space-sm;
  }

  &__label {
    font-size: $ab-text-sm;
    color: rgba(255, 255, 255, 0.85);
  }

  &__tier {
    // badge sits on gradient
  }

  &__balance {
    display: flex;
    align-items: baseline;
    gap: $ab-space-xs;
    margin-bottom: $ab-space-lg;
  }

  &__value {
    font-size: $ab-text-2xl;
    font-weight: $ab-font-bold;
    color: #FFFFFF;
    line-height: 1.1;
  }

  &__unit {
    font-size: $ab-text-sm;
    color: rgba(255, 255, 255, 0.85);
  }

  &__actions {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
  }

  &__signin {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: $ab-space-xs;
    padding: $ab-space-sm $ab-space-md;
    border-radius: $ab-radius-md;
    background-color: rgba(255, 255, 255, 0.95);
    transition: opacity 0.2s ease;

    &:active {
      opacity: 0.85;
    }

    &--done {
      background-color: rgba(255, 255, 255, 0.25);
    }
  }

  &__signin-text {
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-primary;

    .hero__signin--done & {
      color: #FFFFFF;
    }
  }

  &__signin-spinner {
    width: 24rpx;
    height: 24rpx;
    border: 3rpx solid rgba($ab-primary, 0.3);
    border-top-color: $ab-primary;
    border-radius: 50%;
    animation: hero-spin 0.6s linear infinite;
  }

  &__recharge {
    display: flex;
    align-items: center;
    gap: 4rpx;
    padding: $ab-space-sm $ab-space-md;
    border-radius: $ab-radius-md;
    background-color: rgba(255, 255, 255, 0.25);
  }

  &__recharge-icon {
    font-size: $ab-text-md;
  }

  &__recharge-text {
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: #FFFFFF;
  }
}

@keyframes hero-spin {
  to { transform: rotate(360deg); }
}

// --- Section ---
.section {
  margin-bottom: $ab-space-lg;

  &__header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: $ab-space-sm;
    padding: 0 $ab-space-xs;
  }

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }

  &__link {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }
}

// --- Tier cards (horizontal scroll) ---
.tiers {
  width: 100%;
  white-space: nowrap;

  &__track {
    display: inline-flex;
    gap: $ab-space-sm;
    padding: $ab-space-xs $ab-space-xs $ab-space-sm;
  }
}

.tier-card {
  display: inline-flex;
  flex-direction: column;
  width: 440rpx;
  padding: $ab-space-md;
  background-color: $ab-surface;
  border: 4rpx solid $ab-border;
  border-radius: $ab-radius-md;
  box-shadow: $ab-shadow-sm;
  vertical-align: top;
  position: relative;

  &--recommended {
    border-color: $ab-primary;
  }

  &--current {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__head {
    display: flex;
    align-items: center;
    gap: $ab-space-xs;
    margin-bottom: 4rpx;
  }

  &__name {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__rec {
    background-color: $ab-primary;
    border-radius: $ab-radius-full;
    padding: 2rpx 12rpx;
  }

  &__rec-text {
    font-size: $ab-text-xs;
    color: #FFFFFF;
    font-weight: $ab-font-medium;
  }

  &__desc {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    margin-bottom: $ab-space-sm;
    white-space: normal;
  }

  &__row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: $ab-space-xs 0;
    border-bottom: 2rpx solid $ab-divider;

    &:last-of-type {
      border-bottom: none;
    }
  }

  &__row-label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
  }

  &__row-value {
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__platforms {
    display: flex;
    flex-wrap: wrap;
    gap: 8rpx;
    margin-top: $ab-space-sm;
  }

  &__platform {
    font-size: $ab-text-xs;
    color: $ab-text-secondary;
    background-color: $ab-background;
    padding: 4rpx 12rpx;
    border-radius: $ab-radius-sm;
    white-space: normal;
  }

  &__current-badge {
    position: absolute;
    top: -2rpx;
    right: -2rpx;
    background-color: $ab-primary;
    padding: 4rpx 16rpx;
    border-radius: 0 $ab-radius-md 0 $ab-radius-md;
  }

  &__current-badge-text {
    font-size: $ab-text-xs;
    color: #FFFFFF;
    font-weight: $ab-font-medium;
  }
}

// --- Model capabilities ---
.model-grid {
  display: flex;
  // Three equal columns fit comfortably in 750rpx viewport
}

.model-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  padding: $ab-space-md $ab-space-sm;
  border-right: 2rpx solid $ab-divider;

  &:last-child {
    border-right: none;
  }

  &--current {
    background-color: $ab-primary-bg;
  }

  &__name {
    font-size: $ab-text-sm;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    text-align: center;
    margin-bottom: $ab-space-sm;
  }
}

.model-row {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: $ab-space-xs 0;
  gap: 4rpx;

  &__icon {
    width: 40rpx;
    height: 40rpx;
    line-height: 40rpx;
    text-align: center;
    border-radius: 50%;
    font-size: $ab-text-sm;
    font-weight: $ab-font-bold;
    background-color: $ab-background;
    color: $ab-text-tertiary;
  }

  &__label {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    text-align: center;

    &--on {
      color: $ab-text;
    }
  }
}

.model-legend {
  margin-top: $ab-space-sm;
  padding: 0 $ab-space-xs;
}

.model-legend__item {
  display: block;
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
  line-height: 1.6;
}

.model-legend__name {
  font-weight: $ab-font-medium;
  color: $ab-text;
}

.model-legend__dash {
  color: $ab-text-tertiary;
}

.model-legend__desc {
  color: $ab-text-tertiary;
}

// --- Accordion pricing ---
.accordion-item {
  border-bottom: 2rpx solid $ab-divider;

  &--last {
    border-bottom: none;
  }
}

.accordion-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-md;
  min-height: 88rpx;
  box-sizing: border-box;

  &__title {
    flex: 1;
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__arrow {
    font-size: 36rpx;
    color: $ab-text-tertiary;
    transform: rotate(90deg);
    transition: transform 0.2s ease;

    &--open {
      transform: rotate(-90deg);
    }
  }
}

.accordion-body {
  padding: 0 $ab-space-md $ab-space-md;
}

.accordion-footnote {
  display: block;
  margin-top: $ab-space-sm;
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
}

.price-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-sm $ab-space-sm;
  border-radius: $ab-radius-sm;

  &--alt {
    background-color: $ab-background;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding-right: $ab-space-sm;
  }

  &__value {
    font-size: $ab-text-sm;
    font-weight: $ab-font-medium;
    color: $ab-text;
    flex-shrink: 0;
  }
}

.text-group {
  margin-bottom: $ab-space-md;

  &:last-child {
    margin-bottom: 0;
  }

  &__model {
    display: block;
    font-size: $ab-text-xs;
    font-weight: $ab-font-medium;
    color: $ab-text-tertiary;
    margin-bottom: $ab-space-xs;
    padding: 0 $ab-space-sm;
  }
}

.free-box {
  margin-top: $ab-space-sm;
  padding: $ab-space-sm;
  border: 2rpx dashed $ab-border;
  border-radius: $ab-radius-sm;
  background-color: $ab-background;

  &__line {
    display: block;
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.6;
  }
}

// --- Transaction preview ---
.tx-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: $ab-space-md;
  border-bottom: 2rpx solid $ab-divider;

  &--last {
    border-bottom: none;
  }

  &__left {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    flex: 1;
    min-width: 0;
  }

  &__badge {
    width: 64rpx;
    height: 64rpx;
    border-radius: $ab-radius-md;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;

    &--income {
      background-color: $ab-success-bg;
    }

    &--expense {
      background-color: $ab-danger-bg;
    }
  }

  &__badge-icon {
    font-size: 28rpx;
  }

  &__detail {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4rpx;
  }

  &__type {
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__desc {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__right {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    flex-shrink: 0;
    margin-left: $ab-space-sm;
    gap: 4rpx;
  }

  &__amount {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;

    &--income {
      color: $ab-success;
    }

    &--expense {
      color: $ab-danger;
    }
  }

  &__time {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

.tx-more {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: $ab-space-md;
  border-top: 2rpx solid $ab-divider;

  &__text {
    font-size: $ab-text-sm;
    color: $ab-primary;
    font-weight: $ab-font-medium;
  }
}

// --- Bottom spacer ---
.bottom-spacer {
  height: 60rpx;
}

// --- Recharge popup ---
.popup-mask {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  z-index: 999;
  display: flex;
  align-items: flex-end;
  justify-content: center;
}

.popup-sheet {
  width: 100%;
  background-color: $ab-surface;
  border-radius: $ab-radius-lg $ab-radius-lg 0 0;
  padding: $ab-space-md;
  padding-bottom: calc($ab-space-md + env(safe-area-inset-bottom));
  max-height: 80vh;

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: $ab-space-lg;
  }

  &__title {
    font-size: $ab-text-lg;
    font-weight: $ab-font-semibold;
    color: $ab-text;
  }

  &__close {
    font-size: 36rpx;
    color: $ab-text-tertiary;
    padding: $ab-space-xs;
  }
}

.recharge-tiers {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: $ab-space-sm;
  margin-bottom: $ab-space-lg;
}

.recharge-tier {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: $ab-space-md $ab-space-sm;
  border: 4rpx solid $ab-border;
  border-radius: $ab-radius-md;
  background-color: $ab-surface;
  transition: all 0.2s ease;
  position: relative;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__price {
    font-size: $ab-text-md;
    font-weight: $ab-font-bold;
    color: $ab-text;
    margin-bottom: 4rpx;
  }

  &__credits {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-bottom: 4rpx;
  }

  &__bonus {
    font-size: $ab-text-xs;
    color: $ab-danger;
    background-color: $ab-danger-bg;
    padding: 2rpx 12rpx;
    border-radius: $ab-radius-full;
    position: absolute;
    top: -2rpx;
    right: -2rpx;
  }
}

.recharge-divider {
  display: flex;
  align-items: center;
  gap: $ab-space-md;
  margin-bottom: $ab-space-lg;

  &__line {
    flex: 1;
    height: 2rpx;
    background-color: $ab-divider;
  }

  &__text {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    white-space: nowrap;
  }
}

.recharge-qr {
  margin-bottom: $ab-space-lg;

  &__placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    width: 320rpx;
    height: 320rpx;
    margin: 0 auto;
    border: 4rpx dashed $ab-border;
    border-radius: $ab-radius-md;
    background-color: $ab-background;
  }

  &__icon {
    font-size: 80rpx;
    margin-bottom: $ab-space-xs;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-bottom: 4rpx;
  }

  &__hint {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
  }
}

.recharge-footer {
  padding-top: $ab-space-sm;
}
</style>
