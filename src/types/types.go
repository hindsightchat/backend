package types

// other types that arent related to database but may be for like user

type Activity struct {
	SmallText string `json:"small_text"`
	LargeText string `json:"large_text"`

	Details string `json:"details"`
	State   string `json:"state"`
}
