package sdk

// Account 是宿主当前唯一微信账号的业务快照。
//
// 单账号系统不在公共模型中携带账号 ID、tenant、epoch 或运行代次。
type Account struct {
	Username string
	Nickname string
	Online   bool
}
