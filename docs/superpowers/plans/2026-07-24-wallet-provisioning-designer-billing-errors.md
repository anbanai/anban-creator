# 用户钱包开户与 Designer 计费错误实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 保证所有新用户在同一事务中获得空钱包，并让 Designer 使用数值计费错误码、余额预检和可执行的充值提示。

**架构：** 新增应用服务统一编排用户、钱包与邀请计数，认证 Handler 不再直接创建用户。计费服务把缺失钱包归类为账本不变量错误；Studio 公共 HTTP 层按数值业务码翻译错误，Designer 加载钱包并提供价格、余额和充值交互。

**技术栈：** Go、Fiber v3、GORM、SQLite、React 19、TypeScript、TanStack Query、React Router、Sonner、Vitest、Testing Library、Bun。

---

## 文件结构

- 新建 `server/service/user_provisioning.go`：新用户、空钱包和邀请计数的原子开户。
- 新建 `server/service/user_provisioning_test.go`：提交和事务回滚测试。
- 修改 `server/handler/auth.go`、`server/handler/auth_test.go`：四个用户创建入口统一调用开户服务。
- 修改 `server/service/billing_wallet.go`、`server/service/billing_referral.go`、`server/service/billing_wallet_test.go`：所有计费写路径统一执行必需钱包错误分类。
- 修改 `studio/src/lib/http-client.ts`、`studio/src/lib/http-client.test.ts`：数值业务码解析与本地化。
- 修改 `studio/src/pages/DesignerPage.tsx`，新建 `studio/src/pages/DesignerPage.billing.test.tsx`：支付能力交互。

### 任务 1：建立原子用户开户服务

**文件：**
- 新建：`server/service/user_provisioning.go`
- 新建：`server/service/user_provisioning_test.go`
- 修改：`server/handler/auth.go`
- 修改：`server/handler/auth_test.go`

- [ ] **步骤 1：编写开户事务失败测试**

在 `server/service/user_provisioning_test.go` 使用内存 SQLite、`model.AutoMigrate` 和真实仓储编写：

```go
func TestUserProvisioningCreatesUserWalletAndInvitationAtomically(t *testing.T) {
	_, repo := newUserProvisioningFixture(t)
	ctx := context.Background()
	inviter := &model.User{ID: uuid.NewString(), Email: "inviter@example.com", Password: "hashed", InviteCode: "INVITER1"}
	if err := repo.Users().Create(ctx, inviter); err != nil { t.Fatal(err) }
	user := &model.User{ID: uuid.NewString(), Email: "new@example.com", Password: "hashed", InviteCode: "NEWUSER1", InvitedBy: inviter.ID}

	if err := NewUserProvisioningService(repo).Create(ctx, user, inviter.ID, 3); err != nil { t.Fatal(err) }
	account, err := repo.Billing().FindAccount(ctx, user.ID)
	if err != nil { t.Fatalf("wallet: %v", err) }
	if account.PaidCredits != 0 || account.PromotionalCredits != 0 || account.DebtCredits != 0 {
		t.Fatalf("wallet = %+v, want empty", account)
	}
	updatedInviter, err := repo.Users().FindByID(ctx, inviter.ID)
	if err != nil || updatedInviter.InviteCount != 1 { t.Fatalf("inviter = %+v, %v", updatedInviter, err) }
}

func TestUserProvisioningRollsBackWhenWalletCreationFails(t *testing.T) {
	db, repo := newUserProvisioningFixture(t)
	if err := db.Exec(`CREATE TRIGGER fail_wallet_insert BEFORE INSERT ON billing_wallet_accounts BEGIN SELECT RAISE(ABORT, 'forced wallet failure'); END`).Error; err != nil { t.Fatal(err) }
	user := &model.User{ID: uuid.NewString(), Email: "rollback@example.com", Password: "hashed", InviteCode: "ROLLBACK"}
	err := NewUserProvisioningService(repo).Create(context.Background(), user, "", 0)
	if err == nil { t.Fatal("Create error = nil") }
	if _, findErr := repo.Users().FindByID(context.Background(), user.ID); !errors.Is(findErr, gorm.ErrRecordNotFound) {
		t.Fatalf("user survived rollback: %v", findErr)
	}
}

func TestUserProvisioningRollsBackWhenInviteLimitIsReached(t *testing.T) {
	_, repo := newUserProvisioningFixture(t)
	ctx := context.Background()
	inviter := &model.User{ID: uuid.NewString(), Email: "full@example.com", Password: "hashed", InviteCode: "FULLUSER", InviteCount: 1}
	if err := repo.Users().Create(ctx, inviter); err != nil { t.Fatal(err) }
	user := &model.User{ID: uuid.NewString(), Email: "blocked@example.com", Password: "hashed", InviteCode: "BLOCKED1", InvitedBy: inviter.ID}
	err := NewUserProvisioningService(repo).Create(ctx, user, inviter.ID, 1)
	if !errors.Is(err, ErrInviteLimitReached) { t.Fatalf("error = %v", err) }
	if _, findErr := repo.Users().FindByID(ctx, user.ID); !errors.Is(findErr, gorm.ErrRecordNotFound) {
		t.Fatalf("user survived rollback: %v", findErr)
	}
}

func newUserProvisioningFixture(t *testing.T) (*gorm.DB, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil { t.Fatalf("open db: %v", err) }
	if err := model.AutoMigrate(db); err != nil { t.Fatalf("migrate: %v", err) }
	return db, repository.New(db)
}
```

- [ ] **步骤 2：运行测试并确认 RED**

```bash
go test ./server/service -run '^TestUserProvisioning' -count=1
```

预期：编译失败，`NewUserProvisioningService` 和 `ErrInviteLimitReached` 尚未定义。

- [ ] **步骤 3：实现最小开户服务**

在 `server/service/user_provisioning.go` 实现：

```go
package service

import (
	"context"
	"errors"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrInviteLimitReached = errors.New("invite limit reached")

type UserProvisioningService struct { repo repository.Repository }

func NewUserProvisioningService(repo repository.Repository) *UserProvisioningService {
	return &UserProvisioningService{repo: repo}
}

func (s *UserProvisioningService) Create(ctx context.Context, user *model.User, inviterID string, maxInvites int) error {
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.Users().Create(ctx, user); err != nil { return err }
		if err := tx.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: user.ID}); err != nil { return err }
		if inviterID == "" { return nil }
		updated, err := tx.Users().IncrementInviteCount(ctx, inviterID, maxInvites)
		if err != nil { return err }
		if !updated { return ErrInviteLimitReached }
		return nil
	})
}
```

- [ ] **步骤 4：让所有认证创建入口使用开户服务**

在 `AuthHandler` 增加 `userProvisioning *service.UserProvisioningService`，由 `NewAuthHandler` 使用现有仓储初始化。增加：

```go
func (h *AuthHandler) createUser(ctx context.Context, user *model.User, inviter *model.User) error {
	inviterID := ""
	if inviter != nil { inviterID = inviter.ID }
	return h.userProvisioning.Create(ctx, user, inviterID, h.maxInvitePerUser)
}
```

将邮箱注册、验证码登录自动注册、微信登录和二维码登录的四处直接用户写入替换为
`h.createUser(ctx, user, inviter)`。注册入口用 `errors.Is(err, service.ErrInviteLimitReached)`
映射现有邀请上限提示；验证码登录保留 `gorm.ErrDuplicatedKey` 后重新查询用户的并发恢复。

在 `server/handler/auth_test.go` 增加契约测试：

```go
func TestAuthUserCreationUsesProvisioningService(t *testing.T) {
	source, err := os.ReadFile("auth.go")
	if err != nil { t.Fatal(err) }
	text := string(source)
	if got := strings.Count(text, "h.createUser(ctx, user,"); got != 4 {
		t.Fatalf("provisioned user creation calls = %d, want 4", got)
	}
	if strings.Contains(text, "h.repo.Users().Create(ctx, user)") {
		t.Fatal("auth handler still creates users outside provisioning service")
	}
}
```

- [ ] **步骤 5：运行测试并确认 GREEN**

```bash
go test ./server/service -run '^TestUserProvisioning' -count=1
go test ./server/handler -run 'TestAuthUserCreationUsesProvisioningService|TestGenerateTokenPairDoesNotExposeWalletLedger' -count=1
```

预期：全部 PASS。

- [ ] **步骤 6：提交开户变更**

```bash
git add server/service/user_provisioning.go server/service/user_provisioning_test.go server/handler/auth.go server/handler/auth_test.go
git commit -m "fix(auth): provision wallets with new users"
```

### 任务 2：将缺失钱包归类为账本不变量错误

**文件：**
- 修改：`server/service/billing_wallet.go`
- 修改：`server/service/billing_referral.go`
- 修改：`server/service/billing_wallet_test.go`

- [ ] **步骤 1：编写缺失钱包回归测试**

```go
func TestStandaloneChargeClassifiesMissingWalletAsLedgerInvalid(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	if err := model.AutoMigrate(db); err != nil { t.Fatal(err) }
	f := newBillingWalletFixtureWithRepository(t, repository.New(db), 500, 0, 0)
	quote := f.quote(t, billingWalletUserID, "designer.generate_image", "image.designer", "missing-wallet")
	if err := db.Where("user_id = ?", billingWalletUserID).Delete(&model.BillingWalletAccount{}).Error; err != nil { t.Fatal(err) }
	_, err = f.wallet.ChargeStandaloneOperation(context.Background(), OperationChargeRequest{
		UserID: billingWalletUserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID,
		ResourceType: "image_generation", ResourceID: uuid.NewString(), RequestFingerprint: quote.RequestFingerprint,
		IdempotencyScope: "standalone-charge", IdempotencyKey: "missing-wallet",
	})
	if !errors.Is(err, ErrBillingLedgerInvalid) { t.Fatalf("error = %v, want ledger invalid", err) }
	if errors.Is(err, gorm.ErrRecordNotFound) { t.Fatalf("persistence error leaked: %v", err) }
}

func TestBillingServicesDoNotCreateMissingWalletsLazily(t *testing.T) {
	for _, name := range []string{"billing_wallet.go", "billing_referral.go"} {
		source, err := os.ReadFile(name)
		if err != nil { t.Fatal(err) }
		if strings.Contains(string(source), ".EnsureAccount(") || strings.Contains(string(source), "lockOrCreateBillingAccount") {
			t.Fatalf("%s still creates missing wallets lazily", name)
		}
	}
}
```

保留现有 `TestBillingStandaloneRequiresFullBalance`，它负责证明零余额返回
`ErrBillingInsufficientForStandaloneOperation`。

- [ ] **步骤 2：运行测试并确认 RED**

```bash
go test ./server/service -run 'TestStandaloneChargeClassifiesMissingWalletAsLedgerInvalid|TestBillingServicesDoNotCreateMissingWalletsLazily|TestBillingStandaloneRequiresFullBalance' -count=1
```

预期：缺失钱包用例仍得到 `gorm.ErrRecordNotFound`，且源码契约发现惰性开户调用；余额不足用例 PASS。

- [ ] **步骤 3：实现必需钱包锁定辅助函数**

```go
func lockRequiredBillingAccount(ctx context.Context, repo repository.BillingRepository, userID string) (*model.BillingWalletAccount, error) {
	account, err := repo.LockAccount(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: wallet account is missing", ErrBillingLedgerInvalid)
	}
	return account, err
}
```

将任务准入、独立/任务内操作扣费、充值、冲正和钱包维护中所有直接调用 `LockAccount`
或 `lockOrCreateBillingAccount` 的位置替换为该辅助函数，并删除
`lockOrCreateBillingAccount`。在 `billing_referral.go` 的 `lockReferralAccounts` 中删除
`EnsureAccount` 循环，按稳定排序直接调用 `lockRequiredBillingAccount`。这样所有计费写路径
都遵守“钱包由用户开户事务创建”的唯一生命周期契约。

- [ ] **步骤 4：运行测试并确认 GREEN**

```bash
go test ./server/service -run 'TestStandaloneChargeClassifiesMissingWalletAsLedgerInvalid|TestBillingServicesDoNotCreateMissingWalletsLazily|TestBillingStandaloneRequiresFullBalance|TestBillingWallet|TestBillingReferral' -count=1
go test ./server/handler -run 'TestBilling' -count=1
```

预期：全部 PASS，Handler 将缺失钱包映射为现有 `50001/billing_ledger_invalid`。

- [ ] **步骤 5：提交错误边界变更**

```bash
git add server/service/billing_wallet.go server/service/billing_referral.go server/service/billing_wallet_test.go
git commit -m "fix(billing): classify missing wallet invariant"
```

### 任务 3：按数值业务码翻译 Studio 错误

**文件：**
- 修改：`studio/src/lib/http-client.ts`
- 修改：`studio/src/lib/http-client.test.ts`

- [ ] **步骤 1：编写错误码翻译失败测试**

```ts
it.each([
  [40201, 'billing_debt_outstanding', '账户存在欠费，请先充值结清'],
  [40202, 'billing_insufficient_for_task', '积分余额不足，请先充值后继续'],
  [40203, 'billing_insufficient_for_standalone_operation', '积分余额不足，请先充值后继续'],
])('maps billing code %s to localized text', (code, msg, expected) => {
  const error = { response: { data: { code, msg } } }
  expect(getApiErrorMessage(error, '默认错误')).toBe(expected)
  expect(getApiErrorCode(error)).toBe(code)
})

it.each([50000, 50001])('hides internal billing code %s', (code) => {
  const error = { response: { data: { code, msg: 'billing_resource_not_found' } } }
  expect(getApiErrorMessage(error, '图片服务暂时不可用，请稍后重试')).toBe('图片服务暂时不可用，请稍后重试')
})
```

- [ ] **步骤 2：运行测试并确认 RED**

```bash
cd studio && bun run test -- src/lib/http-client.test.ts
```

预期：`getApiErrorCode` 未导出，且当前实现会显示机器标识。

- [ ] **步骤 3：实现类型化 API 错误解析**

```ts
interface ApiErrorBody { code?: number; msg?: string; error?: string }

export function getApiErrorCode(err: unknown): number | undefined {
  if (!err || typeof err !== 'object' || !('response' in err)) return undefined
  const response = (err as { response?: { data?: ApiErrorBody } }).response
  return typeof response?.data?.code === 'number' ? response.data.code : undefined
}

const billingErrorMessages: Partial<Record<number, string>> = {
  40201: '账户存在欠费，请先充值结清',
  40202: '积分余额不足，请先充值后继续',
  40203: '积分余额不足，请先充值后继续',
  40401: '服务计费配置异常，请联系管理员',
  40402: '服务计费配置异常，请联系管理员',
}
```

让 `getApiErrorMessage` 优先读取数值码：公开业务码直接返回中文；`50000`、`50001` 或
任何以 `billing_` 开头的机器消息返回调用方 `fallback`；其他响应继续走现有净化逻辑。

- [ ] **步骤 4：运行测试并确认 GREEN**

```bash
cd studio && bun run test -- src/lib/http-client.test.ts
```

预期：全部 PASS。

- [ ] **步骤 5：提交公共错误处理变更**

```bash
git add studio/src/lib/http-client.ts studio/src/lib/http-client.test.ts
git commit -m "fix(studio): localize billing API errors"
```

### 任务 4：为 Designer 增加支付能力预检与充值操作

**文件：**
- 修改：`studio/src/pages/DesignerPage.tsx`
- 新建：`studio/src/pages/DesignerPage.billing.test.tsx`

- [ ] **步骤 1：编写 Designer 余额交互失败测试**

测试文件复用 provider 测试中的单一启用供应商 fixture，并 mock `designerApi`、
`api.billing.wallet` 与 `sonner.toast`：

```ts
it('shows price and balance and disables generation when credits are insufficient', async () => {
  vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 100, promotional: 0, debt: 0, balance: 100 })
  render(createElement(DesignerPage))
  expect(await screen.findByText('500 积分 · 余额 100')).toBeInTheDocument()
  fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), { target: { value: '生成海报' } })
  expect(screen.getByRole('button', { name: '生成' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '去充值' })).toBeInTheDocument()
})

it('does not disable generation when wallet loading fails', async () => {
  vi.mocked(api.billing.wallet).mockRejectedValue(new Error('network'))
  render(createElement(DesignerPage))
  await screen.findAllByText('GPT Image 2')
  fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), { target: { value: '生成海报' } })
  await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
})

it('offers billing navigation when the server rejects for insufficient credits', async () => {
  vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 1000, promotional: 0, debt: 0, balance: 1000 })
  vi.mocked(designerApi.generate).mockRejectedValue({ response: { data: { code: 40203, msg: 'billing_insufficient_for_standalone_operation' } } })
  render(createElement(DesignerPage))
  await screen.findAllByText('GPT Image 2')
  fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), { target: { value: '生成海报' } })
  fireEvent.click(screen.getByRole('button', { name: '生成' }))
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith(
    '积分余额不足，请先充值后继续',
    expect.objectContaining({ action: expect.objectContaining({ label: '去充值' }) }),
  ))
})
```

- [ ] **步骤 2：运行测试并确认 RED**

```bash
cd studio && bun run test -- src/pages/DesignerPage.billing.test.tsx
```

预期：页面尚未查询钱包、展示价格余额或提供充值操作，因此失败。

- [ ] **步骤 3：实现钱包查询和支付能力状态**

在 `DesignerPage.tsx` 中增加：

```tsx
const navigate = useNavigate()
const { data: wallet, isError: walletError } = useQuery({
  queryKey: ['billing', 'wallet'],
  queryFn: () => api.billing.wallet(),
})
const hasInsufficientCredits = Boolean(
  !walletError && wallet && effectiveProvider && wallet.balance < effectiveProvider.credits,
)
```

将 `submitDisabled` 扩展为：

```tsx
submitDisabled={!promptValue.prompt.trim() || !effectiveProvider || hasInsufficientCredits}
```

非生成状态下在 `status` 显示已加载钱包的
`${effectiveProvider.credits} 积分 · 余额 ${wallet.balance}`；查询失败时只显示本次价格，
不得伪装成零余额。余额不足时在 `trailingTools` 放置小号“去充值”按钮并调用
`navigate('/billing')`。

- [ ] **步骤 4：实现服务端余额不足 Toast 操作**

提取页面内 `showGenerationError` callback，供普通生成与编辑生成复用：

```tsx
const showGenerationError = useCallback((error: unknown) => {
  const message = getApiErrorMessage(error, '图片服务暂时不可用，请稍后重试')
  if (getApiErrorCode(error) === 40203) {
    toast.error(message, { action: { label: '去充值', onClick: () => navigate('/billing') } })
    return
  }
  toast.error(message)
}, [navigate])
```

- [ ] **步骤 5：运行测试并确认 GREEN**

```bash
cd studio && bun run test -- src/pages/DesignerPage.billing.test.tsx src/pages/DesignerPage.provider-contract.test.ts src/components/agent-prompt/AgentPromptInput.test.tsx
```

预期：全部 PASS。

- [ ] **步骤 6：提交 Designer 交互变更**

```bash
git add studio/src/pages/DesignerPage.tsx studio/src/pages/DesignerPage.billing.test.tsx
git commit -m "fix(designer): guide users through image billing"
```

### 任务 5：全量验证

**文件：**
- 不新增文件。

- [ ] **步骤 1：格式化并检查差异**

```bash
gofmt -w server/service/user_provisioning.go server/service/user_provisioning_test.go server/handler/auth.go server/handler/auth_test.go server/service/billing_wallet.go server/service/billing_referral.go server/service/billing_wallet_test.go
git diff --check
```

预期：`git diff --check` 无输出。

- [ ] **步骤 2：运行全部 Go 测试和服务端构建**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
```

预期：两条命令退出码均为 0。

- [ ] **步骤 3：运行全部 Studio 测试和构建**

```bash
cd studio && bun run test
cd studio && bun run build
```

预期：Vitest 全部通过，`tsc -b && vite build` 成功。

- [ ] **步骤 4：检查最终范围**

```bash
git status --short
git diff HEAD~4 --stat
```

预期：只包含本计划列出的实现文件和原本就存在的未跟踪用户文件；没有历史钱包回填、
SKU 价格或供应商路由变更。
