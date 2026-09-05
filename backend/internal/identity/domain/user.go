package domain

// User is the authenticated principal. The plan fixes single-account v1 but
// still scopes everything by owner_id so multi-user works without refactor.
type User struct {
	ID          int64
	Email       string
	DisplayName string
	Timezone    string
	Locale      string
	IsAdmin     bool
}
