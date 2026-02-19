package engine

import "my-go-server/internal/engine/localdb"

func (m *TaskManager) DestroyLocalDB(taskID uint) error {
	if m == nil || taskID == 0 {
		return nil
	}
	return localdb.Default.Destroy(taskID)
}
