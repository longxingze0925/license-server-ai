package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"license-server/internal/model"

	"gorm.io/gorm"
)

// PricingService 计价规则匹配 + 公式求值。
//
// 匹配语义：
//   - 按 priority desc 遍历 enabled=true、provider IN (provider, '*')、scope = scope 的规则；
//   - 对每条规则：把 match_json 解析成 map[string]any，要求 params 里每个 key 对应值都"宽松相等"
//     （字符串化后比较，覆盖 5 == "5" 这类常见情况）。
//   - 第一条全部命中的规则即匹配成功。
//   - 命中后：formula 非空则用公式求值；否则取 credits 字段。
type PricingService struct{}

func NewPricingService() *PricingService { return &PricingService{} }

const (
	legacyDefaultGrokVideoMatchJSON = `{"model":"grok-imagine-video"}`
	defaultGrokVideoMatchJSON       = `{}`
	defaultGrokVideoCredits         = 10
)

// MatchResult 匹配结果。
type MatchResult struct {
	RuleID  int64
	Cost    int
	RuleRef *model.PricingRule
}

// Match 计算给定请求的扣点数。params 通常来自请求 body 中的 {model, duration_seconds, resolution, ...}。
// 未命中任何规则返回 ErrNoPricingRule，调用方应拒绝请求。
func (s *PricingService) Match(provider model.ProviderKind, scope model.PricingScope, params map[string]any) (*MatchResult, error) {
	return s.MatchForTenant("", provider, scope, params)
}

// MatchForTenant 计算当前租户请求的扣点数。
func (s *PricingService) MatchForTenant(tenantID string, provider model.ProviderKind, scope model.PricingScope, params map[string]any) (*MatchResult, error) {
	var rules []model.PricingRule
	q := model.DB.
		Where("scope = ? AND enabled = ? AND (provider = ? OR provider = ?)", scope, true, string(provider), "*").
		Order("priority DESC, id ASC")
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		q = q.Where("tenant_id = ? OR tenant_id = '' OR tenant_id IS NULL", tenantID)
	}
	if err := q.Find(&rules).Error; err != nil {
		return nil, err
	}

	for i := range rules {
		r := &rules[i]
		if !matchAll(r.MatchJSON, params) {
			continue
		}
		cost, err := computeCost(r, params)
		if err != nil {
			return nil, fmt.Errorf("计算公式失败 (rule_id=%d): %w", r.ID, err)
		}
		if cost <= 0 {
			return nil, fmt.Errorf("规则 %d 算出非正扣点：%d", r.ID, cost)
		}
		return &MatchResult{RuleID: r.ID, Cost: cost, RuleRef: r}, nil
	}
	return nil, ErrNoPricingRule
}

// ErrNoPricingRule 未命中任何规则。
var ErrNoPricingRule = errors.New("未匹配到计价规则")

// CRUD - 直接操作 DB，无业务逻辑。

func (s *PricingService) Create(in *model.PricingRule) error {
	in.TenantID = strings.TrimSpace(in.TenantID)
	if in.TenantID == "" {
		return errors.New("tenant_id 不能为空")
	}
	return model.DB.Create(in).Error
}

func (s *PricingService) Update(in *model.PricingRule) error {
	return model.DB.Save(in).Error
}

func (s *PricingService) Delete(id int64) error {
	result := model.DB.Delete(&model.PricingRule{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteForTenant 删除当前租户计价规则。
func (s *PricingService) DeleteForTenant(tenantID string, id int64) error {
	result := model.DB.Delete(&model.PricingRule{}, "id = ? AND tenant_id = ?", id, tenantID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// EnsureDefaultRules 补齐客户端已接入能力所需的最低可用计价规则。
// 已存在自定义同 provider/scope 规则时不覆盖；只升级系统早期生成的旧默认规则。
func (s *PricingService) EnsureDefaultRules() error {
	var tenants []model.Tenant
	if err := model.DB.Find(&tenants).Error; err != nil {
		return err
	}
	for i := range tenants {
		if err := s.ensureDefaultGrokVideoRule(model.DB, tenants[i].ID); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDefaultRulesForTenant 补齐单个租户的默认计价规则。
func (s *PricingService) EnsureDefaultRulesForTenant(tenantID string) error {
	return s.ensureDefaultGrokVideoRule(model.DB, tenantID)
}

func (s *PricingService) EnsureDefaultRulesForTenantTx(tx *gorm.DB, tenantID string) error {
	if tx == nil {
		tx = model.DB
	}
	return s.ensureDefaultGrokVideoRule(tx, tenantID)
}

func (s *PricingService) ensureDefaultGrokVideoRule(db *gorm.DB, tenantID string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil
	}
	var rules []model.PricingRule
	if err := db.
		Where("tenant_id = ? AND provider = ? AND scope = ? AND enabled = ?", tenantID, string(model.ProviderGrok), model.PricingScopeVideo, true).
		Order("priority DESC, id ASC").
		Find(&rules).Error; err != nil {
		return err
	}
	if len(rules) == 0 {
		return db.Create(&model.PricingRule{
			TenantID:  tenantID,
			Provider:  string(model.ProviderGrok),
			Scope:     model.PricingScopeVideo,
			MatchJSON: defaultGrokVideoMatchJSON,
			Credits:   defaultGrokVideoCredits,
			Priority:  10,
			Enabled:   true,
			Note:      defaultGrokVideoRuleNote(),
		}).Error
	}

	for i := range rules {
		if isLegacyDefaultGrokVideoRule(&rules[i]) {
			return db.Model(&model.PricingRule{}).
				Where("id = ?", rules[i].ID).
				Updates(map[string]any{
					"match_json": defaultGrokVideoMatchJSON,
					"note":       defaultGrokVideoRuleNote(),
				}).Error
		}
	}

	return nil
}

func isLegacyDefaultGrokVideoRule(rule *model.PricingRule) bool {
	if rule == nil {
		return false
	}
	if strings.TrimSpace(rule.MatchJSON) != legacyDefaultGrokVideoMatchJSON {
		return false
	}
	if rule.Credits != defaultGrokVideoCredits || strings.TrimSpace(rule.Formula) != "" {
		return false
	}
	return strings.Contains(rule.Note, "默认 Grok 视频计价规则")
}

func defaultGrokVideoRuleNote() string {
	return "默认 Grok 视频计价规则：所有 Grok 视频任务扣 10 点，可在后台按 model/mode/duration/reference_image_count 细化。"
}

func (s *PricingService) Get(id int64) (*model.PricingRule, error) {
	return s.GetForTenant("", id)
}

// GetForTenant 获取当前租户计价规则。tenantID 为空时保留旧内部调用语义。
func (s *PricingService) GetForTenant(tenantID string, id int64) (*model.PricingRule, error) {
	var r model.PricingRule
	q := model.DB.Where("id = ?", id)
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if err := q.First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *PricingService) List(provider, scope string, page, pageSize int) ([]model.PricingRule, int64, error) {
	return s.ListForTenant("", provider, scope, page, pageSize)
}

// ListForTenant 分页列出当前租户计价规则。tenantID 为空时保留旧内部调用语义。
func (s *PricingService) ListForTenant(tenantID, provider, scope string, page, pageSize int) ([]model.PricingRule, int64, error) {
	q := model.DB.Model(&model.PricingRule{})
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	if scope != "" {
		q = q.Where("scope = ?", scope)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	var rows []model.PricingRule
	if err := q.Order("priority DESC, id ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ====================== 内部 ======================

func matchAll(matchJSON string, params map[string]any) bool {
	matchJSON = strings.TrimSpace(matchJSON)
	if matchJSON == "" || matchJSON == "{}" || matchJSON == "null" {
		return true
	}
	var conds map[string]any
	if err := json.Unmarshal([]byte(matchJSON), &conds); err != nil {
		// 规则配错——保守起见，不让它命中
		return false
	}
	for k, want := range conds {
		got, ok := params[k]
		if !ok {
			return false
		}
		if !looseEqual(want, got) {
			return false
		}
	}
	return true
}

func looseEqual(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func computeCost(r *model.PricingRule, params map[string]any) (int, error) {
	if strings.TrimSpace(r.Formula) == "" {
		return r.Credits, nil
	}
	v, err := evalFormula(r.Formula, params)
	if err != nil {
		return 0, err
	}
	if v < 1 {
		return 1, nil // 至少扣 1 点，避免 0 / 负数
	}
	return v, nil
}

// ValidatePricingFormulaSyntax 校验公式能解析，且只使用 extractParams 会收集到的数字变量。
func ValidatePricingFormulaSyntax(src string) error {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	_, err := evalFormula(src, map[string]any{
		"duration_seconds":      1,
		"duration":              1,
		"n":                     1,
		"reference_image_count": 1,
		"input_image_count":     1,
	})
	return err
}

// ====================== 微型公式求值器 ======================
//
// 支持：整数字面量、标识符（取自 params）、+ - * /、括号。
// 不支持：浮点、字符串、函数、变量赋值。
// 例：duration_seconds * 2 + 1
//
// 这个求值器接收的 formula 是后台管理员自己录入的，不是终端用户输入。即便如此，
// 也限制了语法面，避免任何注入风险。

type tokenKind int

const (
	tkNum tokenKind = iota
	tkIdent
	tkPlus
	tkMinus
	tkMul
	tkDiv
	tkLParen
	tkRParen
	tkEOF
)

type token struct {
	kind tokenKind
	num  int
	id   string
}

type lexer struct {
	src string
	pos int
}

func (l *lexer) next() (token, error) {
	for l.pos < len(l.src) && unicode.IsSpace(rune(l.src[l.pos])) {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return token{kind: tkEOF}, nil
	}
	c := l.src[l.pos]
	switch c {
	case '+':
		l.pos++
		return token{kind: tkPlus}, nil
	case '-':
		l.pos++
		return token{kind: tkMinus}, nil
	case '*':
		l.pos++
		return token{kind: tkMul}, nil
	case '/':
		l.pos++
		return token{kind: tkDiv}, nil
	case '(':
		l.pos++
		return token{kind: tkLParen}, nil
	case ')':
		l.pos++
		return token{kind: tkRParen}, nil
	}
	if c >= '0' && c <= '9' {
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		n, err := strconv.Atoi(l.src[start:l.pos])
		if err != nil {
			return token{}, err
		}
		return token{kind: tkNum, num: n}, nil
	}
	if isIdentStart(c) {
		start := l.pos
		for l.pos < len(l.src) && (isIdentStart(l.src[l.pos]) || (l.src[l.pos] >= '0' && l.src[l.pos] <= '9')) {
			l.pos++
		}
		return token{kind: tkIdent, id: l.src[start:l.pos]}, nil
	}
	return token{}, fmt.Errorf("非法字符 %q at %d", c, l.pos)
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

type parser struct {
	lex    *lexer
	cur    token
	params map[string]any
}

func newParser(src string, params map[string]any) (*parser, error) {
	p := &parser{lex: &lexer{src: src}, params: params}
	tk, err := p.lex.next()
	if err != nil {
		return nil, err
	}
	p.cur = tk
	return p, nil
}

func (p *parser) advance() error {
	tk, err := p.lex.next()
	if err != nil {
		return err
	}
	p.cur = tk
	return nil
}

// expr := term (('+'|'-') term)*
func (p *parser) parseExpr() (int, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.cur.kind == tkPlus || p.cur.kind == tkMinus {
		op := p.cur.kind
		if err := p.advance(); err != nil {
			return 0, err
		}
		r, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == tkPlus {
			v += r
		} else {
			v -= r
		}
	}
	return v, nil
}

// term := factor (('*'|'/') factor)*
func (p *parser) parseTerm() (int, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for p.cur.kind == tkMul || p.cur.kind == tkDiv {
		op := p.cur.kind
		if err := p.advance(); err != nil {
			return 0, err
		}
		r, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if op == tkMul {
			v *= r
		} else {
			if r == 0 {
				return 0, errors.New("除以零")
			}
			v /= r
		}
	}
	return v, nil
}

// factor := number | ident | '(' expr ')' | '-' factor
func (p *parser) parseFactor() (int, error) {
	switch p.cur.kind {
	case tkNum:
		n := p.cur.num
		if err := p.advance(); err != nil {
			return 0, err
		}
		return n, nil
	case tkIdent:
		name := p.cur.id
		if err := p.advance(); err != nil {
			return 0, err
		}
		v, ok := p.params[name]
		if !ok {
			return 0, fmt.Errorf("公式变量 %q 在请求参数里未提供", name)
		}
		n, err := toInt(v)
		if err != nil {
			return 0, fmt.Errorf("公式变量 %q 不是整数: %w", name, err)
		}
		return n, nil
	case tkLParen:
		if err := p.advance(); err != nil {
			return 0, err
		}
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.cur.kind != tkRParen {
			return 0, errors.New("缺少右括号")
		}
		if err := p.advance(); err != nil {
			return 0, err
		}
		return v, nil
	case tkMinus:
		if err := p.advance(); err != nil {
			return 0, err
		}
		v, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -v, nil
	}
	return 0, fmt.Errorf("意外的 token (kind=%d)", p.cur.kind)
}

func evalFormula(src string, params map[string]any) (int, error) {
	p, err := newParser(src, params)
	if err != nil {
		return 0, err
	}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.cur.kind != tkEOF {
		return 0, errors.New("公式末尾有残余字符")
	}
	return v, nil
}

func toInt(v any) (int, error) {
	switch x := v.(type) {
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		return int(x), nil
	case float32:
		return int(x), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(x))
	case json.Number:
		n, err := x.Int64()
		if err == nil {
			return int(n), nil
		}
		f, err := x.Float64()
		return int(f), err
	}
	return 0, fmt.Errorf("无法转换为整数: %T", v)
}
