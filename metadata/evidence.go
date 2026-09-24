package metadata

// JudgmentInput 只保留作品身份与实际人工约束；没有人工指定时省略零值引用。
type JudgmentInput struct {
	Kind  Kind  `json:"kind"`
	Ref   Ref   `json:"ref,omitzero"`
	Query Query `json:"query"`
}

func JudgmentInputFor(request Request) JudgmentInput {
	return JudgmentInput{Kind: request.Kind, Ref: request.Ref, Query: request.Query}
}
