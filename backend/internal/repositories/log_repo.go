package repositories

import (
	"database/sql"
	"ops-admin-backend/internal/models"
	"ops-admin-backend/internal/utils"
)

type LogRepository struct {
	db *sql.DB
}

func NewLogRepository(db *sql.DB) *LogRepository {
	return &LogRepository{db: db}
}

func (r *LogRepository) LogAction(userID int64, username, action, projectType, detail string) {
	detail = utils.NormalizeGarbledText(detail)
	_, _ = r.db.Exec(
		`INSERT INTO operation_logs(user_id,username,action,project_type,detail,created_at) VALUES(?,?,?,?,?,?)`,
		userID, username, action, projectType, detail, utils.NowStr(),
	)
}

func (r *LogRepository) GetLogs(page, pageSize int, projectType string) (*models.LogsResponse, error) {
	offset := (page - 1) * pageSize

	where := ""
	args := make([]interface{}, 0)
	if projectType != "" {
		where = ` WHERE project_type=?`
		args = append(args, projectType)
	}

	var total int64
	countQuery := `SELECT COUNT(1) FROM operation_logs` + where
	if err := r.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	query := `SELECT id,COALESCE(user_id,0),COALESCE(username,''),COALESCE(action,''),COALESCE(project_type,''),COALESCE(detail,''),created_at FROM operation_logs` +
		where + ` ORDER BY id DESC LIMIT ? OFFSET ?`

	finalArgs := make([]interface{}, 0, len(args)+2)
	finalArgs = append(finalArgs, args...)
	finalArgs = append(finalArgs, pageSize, offset)

	rows, err := r.db.Query(query, finalArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]models.LogRow, 0)
	for rows.Next() {
		var row models.LogRow
		if err = rows.Scan(&row.ID, &row.UserID, &row.Username, &row.Action, &row.ProjectType, &row.Detail, &row.CreatedAt); err != nil {
			return nil, err
		}
		row.Detail = utils.NormalizeGarbledText(row.Detail)
		items = append(items, row)
	}

	return &models.LogsResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
