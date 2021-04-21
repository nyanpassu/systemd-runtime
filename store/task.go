package store

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
)

const bucketTask = "tasks"

type Task struct {
	ID         string
	BundlePath string
	Namespace  string
}

type TaskStore interface {
	Create(context.Context, *Task) error
	Retrieve(context.Context, *Task) error
	RetrieveAll(context.Context) ([]Task, error)
	Update(context.Context, Task) error
	Delete(context.Context, Task) error
}

type dbTaskStore struct {
	db *bolt.DB
}

func (ts *dbTaskStore) Create(ctx context.Context, task *Task) error {
	content, err := json.Marshal(task)
	if err != nil {
		return err
	}

	return ts.db.Update(func(tx *bolt.Tx) error {
		key := []byte(task.ID)
		b := tx.Bucket([]byte(bucketTask))

		value := b.Get(key)
		if value != nil {
			return errors.Errorf("task %s already exists", task.ID)
		}

		return b.Put(key, content)
	})
}

func (ts *dbTaskStore) Retrieve(ctx context.Context, task *Task) error {
	return ts.db.View(func(tx *bolt.Tx) error {
		key := []byte(task.ID)
		b := tx.Bucket([]byte(bucketTask))

		value := b.Get(key)
		if value == nil {
			return errors.Errorf("task %s not exists", task.ID)
		}
		return json.Unmarshal(value, task)
	})
}

func (ts *dbTaskStore) RetrieveAll(context.Context) ([]Task, error) {
	var tasks []Task
	if err := ts.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketTask))
		return b.ForEach(func(k, v []byte) error {
			t := Task{}
			return json.Unmarshal(v, &t)
		})
	}); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (ts *dbTaskStore) Update(ctx context.Context, task Task) error {
	content, err := json.Marshal(task)
	if err != nil {
		return err
	}

	return ts.db.Update(func(tx *bolt.Tx) error {
		key := []byte(task.ID)
		b := tx.Bucket([]byte(bucketTask))

		value := b.Get(key)
		if value != nil {
			return errors.Errorf("task %s already exists", task.ID)
		}

		return b.Put(key, content)
	})
}

func (ts *dbTaskStore) Delete(ctx context.Context, task Task) error {
	return ts.db.Update(func(tx *bolt.Tx) error {
		key := []byte(task.ID)
		b := tx.Bucket([]byte(bucketTask))

		return b.Delete(key)
	})
}
