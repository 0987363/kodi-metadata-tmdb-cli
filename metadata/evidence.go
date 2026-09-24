package metadata

// JudgmentInput 只保留作品身份与人工来源约束，不把未验证编号提示作为判断证据。
type JudgmentInput struct {
	Kind  Kind  `json:"kind"`
	Ref   Ref   `json:"ref"`
	Query Query `json:"query"`
}

func JudgmentInputFor(request Request) JudgmentInput {
	return JudgmentInput{Kind: request.Kind, Ref: request.Ref, Query: request.Query}
}
