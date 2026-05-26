package handler

import (
	"net/http"

	"github.com/google/uuid"

	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
)

// MeHandler отдаёт информацию о текущем аутентифицированном сотруднике.
// Данные берёт из context.Context, заполненного мидлварью Auth, поэтому
// дополнительного похода в БД на стороне хендлера не требуется.
type MeHandler struct{}

func NewMeHandler() *MeHandler {
	return &MeHandler{}
}

type meResponse struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	Role       string    `json:"role"`
	Status     string    `json:"status"`
}

func (h *MeHandler) Get(w http.ResponseWriter, r *http.Request) {
	principal, ok := mw.PrincipalFromContext(r.Context())
	if !ok {
		// Сюда мы попасть не должны: мидлварь Auth гарантирует наличие Principal
		// до вызова хендлера. Защитная ветка — на случай ошибки конфигурации
		// роутера (хендлер привинчен мимо мидлвари).
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "principal missing in context")
		return
	}

	response.WriteJSON(w, http.StatusOK, meResponse{
		EmployeeID: principal.EmployeeID,
		Role:       principal.Role,
		Status:     principal.Status,
	})
}
