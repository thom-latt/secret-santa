package api

import "time"

type PublicConfigDTO struct {
	AllowPublicEventCreation bool `json:"allow_public_event_creation"`
}

type CreateEventRequest struct {
	Title     string `json:"title"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Locale    string `json:"locale"`
}

type CreateEventResponse struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	AdminToken string `json:"admin_token"`
}

type RecoverLinksRequest struct {
	Email  string `json:"email"`
	Locale string `json:"locale"`
}

type GenericMessageResponse struct {
	Message string `json:"message"`
}

// AdminParticipantDTO strictly omits assigned_to_id and participant_token (except for the organizer's own token on the event level).
type AdminParticipantDTO struct {
	ID            string     `json:"id"`
	FirstName     string     `json:"first_name"`
	LastName      string     `json:"last_name"`
	Email         string     `json:"email"`
	IsOrganizer   bool       `json:"is_organizer"`
	LinkClickedAt *time.Time `json:"link_clicked_at"`
	EmailSentAt   *time.Time `json:"email_sent_at"`
	EmailError    *string    `json:"email_error"`
	WishlistCount int        `json:"wishlist_count"`
}

type ExclusionDTO struct {
	ID             string `json:"id"`
	ParticipantAID string `json:"participant_a_id"`
	ParticipantBID string `json:"participant_b_id"`
}

type FeasibilityDTO struct {
	Mode       string `json:"mode"`       // "single_cycle" | "sub_loops" | "impossible" | "too_few"
	Diagnostic string `json:"diagnostic"`
}

type AdminEventResponseDTO struct {
	ID                        string                `json:"id"`
	Title                     string                `json:"title"`
	Description               string                `json:"description"`
	EventDate                 string                `json:"event_date"`
	Budget                    string                `json:"budget"`
	OrganizerFirstName        string                `json:"organizer_first_name"`
	OrganizerLastName         string                `json:"organizer_last_name"`
	OrganizerEmail            string                `json:"organizer_email"`
	OrganizerParticipates     bool                  `json:"organizer_participates"`
	OrganizerParticipantToken string                `json:"organizer_participant_token,omitempty"`
	Status                    string                `json:"status"`
	Locale                    string                `json:"locale"`
	CreatedAt                 time.Time             `json:"created_at"`
	DrawnAt                   *time.Time            `json:"drawn_at"`
	Participants              []AdminParticipantDTO `json:"participants"`
	Exclusions                []ExclusionDTO        `json:"exclusions"`
	Feasibility               FeasibilityDTO        `json:"feasibility"`
}

type UpdateEventRequest struct {
	Title                 string `json:"title"`
	Description           string `json:"description"`
	EventDate             string `json:"event_date"`
	Budget                string `json:"budget"`
	OrganizerParticipates *bool  `json:"organizer_participates"`
}

type UpsertParticipantRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

type CreateExclusionRequest struct {
	ParticipantAID string `json:"participant_a_id"`
	ParticipantBID string `json:"participant_b_id"`
}

type WishlistItemDTO struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Note      string    `json:"note"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TargetDTO struct {
	FirstName string            `json:"first_name"`
	LastName  string            `json:"last_name"`
	Wishlist  []WishlistItemDTO `json:"wishlist"`
}

// ParticipantSelfDTO exposes Target ONLY when LinkClickedAt != nil.
type ParticipantSelfDTO struct {
	FirstName       string            `json:"first_name"`
	LastName        string            `json:"last_name"`
	Email           string            `json:"email"`
	EventTitle      string            `json:"event_title"`
	EventDate       string            `json:"event_date"`
	Budget          string            `json:"budget"`
	Description     string            `json:"description"`
	Locale          string            `json:"locale"`
	Revealed        bool              `json:"revealed"`
	LinkClickedAt   *time.Time        `json:"link_clicked_at"`
	Target          *TargetDTO        `json:"target,omitempty"`
	OwnWishlist     []WishlistItemDTO `json:"own_wishlist"`
}

type UpsertWishlistItemRequest struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Note  string `json:"note"`
}

type SuperAdminLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type SuperAdminSettingsRequest struct {
	AllowPublicEventCreation bool `json:"allow_public_event_creation"`
}

// SuperAdminEventDTO strictly omits any draw assignment data.
type SuperAdminEventDTO struct {
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	OrganizerFirstName   string     `json:"organizer_first_name"`
	OrganizerLastName    string     `json:"organizer_last_name"`
	OrganizerEmail       string     `json:"organizer_email"`
	Status               string     `json:"status"`
	Locale               string     `json:"locale"`
	CreatedAt            time.Time  `json:"created_at"`
	DrawnAt              *time.Time `json:"drawn_at"`
	ParticipantCount     int        `json:"participant_count"`
	RevealedTargetsCount int        `json:"revealed_targets_count"`
	FilledWishlistsCount int        `json:"filled_wishlists_count"`
}

type SuperAdminOverviewDTO struct {
	AllowPublicEventCreation bool                 `json:"allow_public_event_creation"`
	SuperAdminEmail          string               `json:"super_admin_email"`
	Events                   []SuperAdminEventDTO `json:"events"`
}
