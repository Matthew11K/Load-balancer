package postgres

import (
	"context"

	"balancer/internal/domain/ratelimit"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientRepository struct {
	pool *pgxpool.Pool
}

func NewClientRepository(pool *pgxpool.Pool) *ClientRepository {
	return &ClientRepository{
		pool: pool,
	}
}

func (r *ClientRepository) SaveClient(clientData *ratelimit.ClientData) error {
	ctx := context.Background()

	const query = `
		INSERT INTO clients (id, capacity, rate_per_sec, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET capacity = $2, rate_per_sec = $3, updated_at = $5
	`

	_, err := r.pool.Exec(ctx, query,
		clientData.ID,
		clientData.Capacity,
		clientData.RatePerSec,
		clientData.CreatedAt,
		clientData.UpdatedAt,
	)

	return err
}

func (r *ClientRepository) GetClient(clientID string) (*ratelimit.ClientData, error) {
	ctx := context.Background()

	const query = `
		SELECT id, capacity, rate_per_sec, created_at, updated_at
		FROM clients
		WHERE id = $1
	`

	var clientData ratelimit.ClientData
	err := r.pool.QueryRow(ctx, query, clientID).Scan(
		&clientData.ID,
		&clientData.Capacity,
		&clientData.RatePerSec,
		&clientData.CreatedAt,
		&clientData.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &clientData, nil
}

func (r *ClientRepository) DeleteClient(clientID string) error {
	ctx := context.Background()

	const query = `
		DELETE FROM clients
		WHERE id = $1
	`

	_, err := r.pool.Exec(ctx, query, clientID)

	return err
}

func (r *ClientRepository) ListClients() ([]*ratelimit.ClientData, error) {
	ctx := context.Background()

	const query = `
		SELECT id, capacity, rate_per_sec, created_at, updated_at
		FROM clients
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []*ratelimit.ClientData

	for rows.Next() {
		var client ratelimit.ClientData

		err := rows.Scan(
			&client.ID,
			&client.Capacity,
			&client.RatePerSec,
			&client.CreatedAt,
			&client.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		clients = append(clients, &client)
	}

	return clients, rows.Err()
}
