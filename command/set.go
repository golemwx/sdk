package command

import (
	"bytes"
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/validation"
)

var (
	durationValueType   = reflect.TypeOf(time.Duration(0))
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// Set 是一个由结构体声明生成、可重复解析的不可变命令树。
type Set struct {
	root   reflect.Type
	schema Schema
}

// NewSet 从结构体命令声明生成 schema。Set 不保存任何一次调用的绑定值。
func NewSet(commands any) (*Set, error) {
	root, err := commandRootType(commands)
	if err != nil {
		return nil, err
	}
	schema, err := buildSchema(root)
	if err != nil {
		return nil, err
	}
	return &Set{root: root, schema: schema}, nil
}

func commandRootType(commands any) (reflect.Type, error) {
	if commands == nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令声明不能为空")
	}
	value := reflect.ValueOf(commands)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令声明不能是空指针")
	}
	root := value.Type()
	for root.Kind() == reflect.Pointer {
		root = root.Elem()
	}
	if root.Kind() != reflect.Struct {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令声明必须是结构体")
	}
	return root, nil
}

// Schema 返回命令树的独立副本。
func (s *Set) Schema() Schema {
	if s == nil {
		return Schema{}
	}
	return cloneSchema(s.schema)
}

// Parse 从结构体声明创建一次独立命令值，完成 Kong 绑定和 selected 节点校验。
//
// owner 是 frame 已校验的路由结果；owner 命令在解析前和解析后都会再次检查，
// 防止 downstream 调用绕过权限。
func (s *Set) Parse(tokens []string, owner bool) (*Call, error) {
	if s == nil {
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.command", "命令树未初始化")
	}
	tokens = normalizeTokens(tokens)
	helpRequested, parseTokens := splitHelp(tokens)
	if !owner && s.requiresOwner(parseTokens) {
		return nil, permissionDenied()
	}
	if helpRequested {
		return s.help(parseTokens)
	}

	parser, err := newKong(s.root, io.Discard, false)
	if err != nil {
		return nil, err
	}
	kongContext, err := parser.Parse(parseTokens)
	if err != nil {
		return nil, wrapParseError(err)
	}
	selected := selectedCommand(kongContext)
	if selected == nil {
		return nil, sdk.NewError(sdk.CategoryNotFound, "plugin.command", "未找到可执行的命令")
	}
	if !owner && pathRequiresOwner(kongContext) {
		return nil, permissionDenied()
	}
	if selected.Target.IsValid() && selected.Target.CanInterface() {
		if err := validation.Struct(selected.Target.Interface(), "plugin.command.validate"); err != nil {
			return nil, err
		}
	}
	return &Call{context: kongContext}, nil
}

// Call 是一次解析后的独立命令调用。
type Call struct {
	context *kong.Context
	help    string
}

// Help 返回本次调用的帮助文本；非帮助请求为空。
func (c *Call) Help() string {
	if c == nil {
		return ""
	}
	return c.help
}

// Run 调用 selected 命令的 Run 方法。ctx 和显式绑定会注入 Run 参数，
// command.Reply 同时按接口类型绑定。
func (c *Call) Run(ctx context.Context, bindings ...any) error {
	if c == nil || c.context == nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.command", "命令调用未完成解析")
	}
	if ctx == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "命令上下文不能为空")
	}
	c.context.BindTo(ctx, (*context.Context)(nil))
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		c.context.Bind(binding)
		if reply, ok := binding.(Reply); ok {
			c.context.BindTo(reply, (*Reply)(nil))
		}
	}
	if err := c.context.Run(); err != nil {
		if strings.Contains(err.Error(), "no Run() method found") {
			return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.command", "命令没有 Run 处理函数")
		}
		return err
	}
	return nil
}

func normalizeTokens(tokens []string) []string {
	if tokens == nil {
		return nil
	}
	normalized := slices.Clone(tokens)
	if len(normalized) > 0 {
		normalized[0] = strings.TrimPrefix(normalized[0], "/")
	}
	return normalized
}

func splitHelp(tokens []string) (bool, []string) {
	if len(tokens) == 0 {
		return false, tokens
	}
	remaining := make([]string, 0, len(tokens))
	requested := false
	for index, token := range tokens {
		if token == "--" {
			remaining = append(remaining, tokens[index:]...)
			break
		}
		if token == "--help" || token == "-h" {
			requested = true
			continue
		}
		remaining = append(remaining, token)
	}
	return requested, remaining
}

func newKong(root reflect.Type, output io.Writer, defaultHelp bool) (*kong.Kong, error) {
	options := []kong.Option{
		kong.Name("golem"),
		kong.Writers(output, output),
		kong.Exit(func(int) {}),
	}
	if !defaultHelp {
		options = append(options, kong.NoDefaultHelp())
	}
	parser, err := kong.New(reflect.New(root).Interface(), options...)
	if err != nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令声明无效："+err.Error())
	}
	return parser, nil
}

func (s *Set) help(tokens []string) (*Call, error) {
	var output bytes.Buffer
	parser, err := newKong(s.root, &output, true)
	if err != nil {
		return nil, err
	}
	kongContext, err := kong.Trace(parser, tokens)
	if err != nil {
		return nil, wrapParseError(err)
	}
	if kongContext.Error != nil {
		return nil, wrapParseError(kongContext.Error)
	}
	if err := kongContext.PrintUsage(false); err != nil {
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.command.help", "生成帮助失败")
	}
	text := strings.TrimSpace(output.String())
	if text == "" {
		text = "没有可用的帮助信息"
	}
	return &Call{help: text}, nil
}

func selectedCommand(context *kong.Context) *kong.Node {
	if context == nil {
		return nil
	}
	for index := len(context.Path) - 1; index >= 0; index-- {
		if command := context.Path[index].Command; command != nil {
			return command
		}
	}
	if context.Model != nil && context.Model.Node != nil {
		return context.Model.Node
	}
	return nil
}

func pathRequiresOwner(context *kong.Context) bool {
	if context == nil {
		return false
	}
	for _, path := range context.Path {
		if path.Command == nil {
			continue
		}
		if scope, err := scopeFromTag(path.Command.Tag); err == nil && scope == ScopeOwner {
			return true
		}
	}
	return false
}

func permissionDenied() error {
	return sdk.NewError(sdk.CategoryPermissionDenied, "plugin.command", "只有 owner 可以执行该命令")
}

func (s *Set) requiresOwner(tokens []string) bool {
	index := 0
	for index < len(tokens) && (tokens[index] == "" || strings.HasPrefix(tokens[index], "-")) {
		index++
	}
	if index >= len(tokens) {
		return false
	}
	node, ok := findCommandNode(s.schema.Roots, tokens[index])
	if !ok {
		return false
	}
	if node.Scope == ScopeOwner {
		return true
	}
	for index++; index < len(tokens); index++ {
		token := tokens[index]
		if token == "" || strings.HasPrefix(token, "-") {
			return false
		}
		child, exists := findCommandNode(node.Children, token)
		if !exists {
			return false
		}
		if child.Scope == ScopeOwner {
			return true
		}
		node = child
	}
	return false
}

func findCommandNode(nodes []Node, token string) (Node, bool) {
	for _, node := range nodes {
		if nodeMatches(node, token) {
			return node, true
		}
	}
	return Node{}, false
}

func nodeMatches(node Node, token string) bool {
	return node.Name == token || slices.Contains(node.Aliases, token)
}

func wrapParseError(err error) error {
	if err == nil {
		return nil
	}
	var business *sdk.Error
	if errors.As(err, &business) {
		return business
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "unknown flag"):
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "未知参数")
	case strings.Contains(text, "unknown command"):
		return sdk.NewError(sdk.CategoryNotFound, "plugin.command", "未知命令")
	case strings.Contains(text, "is required"):
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "缺少必填参数")
	case strings.Contains(text, "missing flags"):
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "缺少必填参数")
	case strings.Contains(text, "missing positional arguments"):
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "缺少必填参数")
	case strings.Contains(text, "expected one of"), strings.Contains(text, "must be one of"):
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "参数值无效")
	case strings.HasPrefix(text, "expected "):
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "缺少必填参数")
	default:
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "命令参数无效，请检查参数值、枚举、必填项和类型")
	}
}

func cloneSchema(value Schema) Schema {
	result := Schema{Roots: make([]Node, len(value.Roots))}
	for index, root := range value.Roots {
		result.Roots[index] = cloneNode(root)
	}
	return result
}

func cloneNode(value Node) Node {
	result := value
	result.Aliases = slices.Clone(value.Aliases)
	result.Arguments = make([]Argument, len(value.Arguments))
	for index, argument := range value.Arguments {
		result.Arguments[index] = cloneArgument(argument)
	}
	result.Children = make([]Node, len(value.Children))
	for index, child := range value.Children {
		result.Children[index] = cloneNode(child)
	}
	return result
}

func cloneArgument(value Argument) Argument {
	result := value
	result.Enum = slices.Clone(value.Enum)
	return result
}

func buildSchema(root reflect.Type) (Schema, error) {
	parser, err := newKong(root, io.Discard, false)
	if err != nil {
		return Schema{}, err
	}
	schema := Schema{Roots: make([]Node, 0, len(parser.Model.Children))}
	for _, child := range parser.Model.Children {
		node, err := nodeFromKong(child, ScopeUser)
		if err != nil {
			return Schema{}, err
		}
		schema.Roots = append(schema.Roots, node)
	}
	if err := validateSchema(schema); err != nil {
		return Schema{}, err
	}
	return schema, nil
}

func nodeFromKong(value *kong.Node, inherited Scope) (Node, error) {
	if value == nil {
		return Node{}, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令节点不能为空")
	}
	scope, err := scopeFromTag(value.Tag)
	if err != nil {
		return Node{}, err
	}
	if inherited == ScopeOwner {
		scope = ScopeOwner
	}
	node := Node{
		Name:      value.Name,
		Aliases:   slices.Clone(value.Aliases),
		Help:      value.Help,
		Scope:     scope,
		Arguments: make([]Argument, 0, len(value.Flags)+len(value.Positional)),
		Children:  make([]Node, 0, len(value.Children)),
	}
	for _, flag := range value.Flags {
		argument, err := argumentFromValue(flag.Value, flag)
		if err != nil {
			return Node{}, err
		}
		node.Arguments = append(node.Arguments, argument)
	}
	for _, positional := range value.Positional {
		argument, err := argumentFromValue(positional, nil)
		if err != nil {
			return Node{}, err
		}
		node.Arguments = append(node.Arguments, argument)
	}
	for _, child := range value.Children {
		converted, err := nodeFromKong(child, scope)
		if err != nil {
			return Node{}, err
		}
		node.Children = append(node.Children, converted)
	}
	return node, nil
}

func argumentFromValue(value *kong.Value, flag *kong.Flag) (Argument, error) {
	if value == nil {
		return Argument{}, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令参数不能为空")
	}
	valueType, repeated, err := schemaValueType(value)
	if err != nil {
		return Argument{}, err
	}
	argument := Argument{
		Name:       value.Name,
		Positional: flag == nil,
		Type:       valueType,
		Required:   value.Required,
		Repeated:   repeated,
		Default:    value.Default,
		Help:       value.Help,
	}
	if value.Enum != "" {
		argument.Enum = value.EnumSlice()
	}
	if flag != nil && flag.Short != 0 {
		argument.Short = string(flag.Short)
	}
	return argument, nil
}

func schemaValueType(value *kong.Value) (ValueType, bool, error) {
	if value.Enum != "" {
		return TypeEnum, value.IsSlice(), nil
	}
	target := value.Target.Type()
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	repeated := false
	if target.Kind() == reflect.Slice {
		repeated = true
		target = target.Elem()
		for target.Kind() == reflect.Pointer {
			target = target.Elem()
		}
	}
	if target.Kind() == reflect.Map {
		return TypeMap, false, nil
	}
	if target == durationValueType {
		return TypeDuration, repeated, nil
	}
	if target.Implements(textUnmarshalerType) || reflect.PointerTo(target).Implements(textUnmarshalerType) {
		return TypeString, repeated, nil
	}
	switch target.Kind() {
	case reflect.String:
		return TypeString, repeated, nil
	case reflect.Bool:
		return TypeBool, repeated, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return TypeInt, repeated, nil
	case reflect.Float32, reflect.Float64:
		return TypeFloat, repeated, nil
	default:
		return TypeUnknown, repeated, sdk.NewError(
			sdk.CategoryInvalidArgument,
			"plugin.command.schema",
			fmt.Sprintf("命令参数 %q 的类型 %s 不受支持", value.Name, target),
		)
	}
}

func scopeFromTag(tag *kong.Tag) (Scope, error) {
	if tag == nil {
		return ScopeUser, nil
	}
	switch value := tag.Get("scope"); value {
	case "", "user":
		return ScopeUser, nil
	case "owner":
		return ScopeOwner, nil
	default:
		return ScopeUnknown, sdk.NewError(
			sdk.CategoryInvalidArgument,
			"plugin.command.schema",
			fmt.Sprintf("命令 scope %q 无效", value),
		)
	}
}

func validateSchema(schema Schema) error {
	return validateSiblings(schema.Roots)
}

func validateSiblings(nodes []Node) error {
	names := make(map[string]string, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.Name) == "" {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令名称不能为空")
		}
		if err := registerCommandName(names, node.Name, node.Name); err != nil {
			return err
		}
		for _, alias := range node.Aliases {
			if strings.TrimSpace(alias) == "" {
				return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令别名不能为空")
			}
			if err := registerCommandName(names, alias, node.Name); err != nil {
				return err
			}
		}
		if err := validateArguments(node); err != nil {
			return err
		}
		if err := validateSiblings(node.Children); err != nil {
			return err
		}
	}
	return nil
}

func registerCommandName(names map[string]string, name, owner string) error {
	if previous, exists := names[name]; exists {
		return sdk.NewError(
			sdk.CategoryInvalidArgument,
			"plugin.command.schema",
			fmt.Sprintf("命令根或别名 %q 重复（%s 与 %s）", name, previous, owner),
		)
	}
	names[name] = owner
	return nil
}

func validateArguments(node Node) error {
	names := make(map[string]struct{}, len(node.Arguments))
	shorts := make(map[string]struct{}, len(node.Arguments))
	for _, argument := range node.Arguments {
		if argument.Name == "" {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command.schema", "命令参数名称不能为空")
		}
		if _, exists := names[argument.Name]; exists {
			return sdk.NewError(
				sdk.CategoryInvalidArgument,
				"plugin.command.schema",
				fmt.Sprintf("命令 %q 的参数 %q 重复", node.Name, argument.Name),
			)
		}
		names[argument.Name] = struct{}{}
		if argument.Name == "help" {
			return sdk.NewError(
				sdk.CategoryInvalidArgument,
				"plugin.command.schema",
				fmt.Sprintf("命令 %q 的参数 help 与自动帮助冲突", node.Name),
			)
		}
		if argument.Short == "" {
			continue
		}
		if argument.Short == "h" {
			return sdk.NewError(
				sdk.CategoryInvalidArgument,
				"plugin.command.schema",
				fmt.Sprintf("命令 %q 的短参数 -h 与自动帮助冲突", node.Name),
			)
		}
		if _, exists := shorts[argument.Short]; exists {
			return sdk.NewError(
				sdk.CategoryInvalidArgument,
				"plugin.command.schema",
				fmt.Sprintf("命令 %q 的短参数 %q 重复", node.Name, argument.Short),
			)
		}
		shorts[argument.Short] = struct{}{}
	}
	return nil
}
