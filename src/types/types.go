package types

// other types that arent related to database but may be for like user

type ActivityTimestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type Activity struct {
	SmallText string `json:"small_text"`
	LargeText string `json:"large_text"`

	Details string `json:"details"`
	State   string `json:"state"`

	AppName string `json:"app_name"`

	Timestamps *ActivityTimestamps `json:"timestamps,omitempty"`
}
