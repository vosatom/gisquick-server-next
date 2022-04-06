package domain

type User struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	IsSuperuser     bool   `json:"is_superuser"`
	IsAuthenticated bool   `json:"-"`
	IsGuest         bool   `json:"is_guest"`
}
