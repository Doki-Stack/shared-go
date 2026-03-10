package health

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

// Check verifies the health of a single dependency.
type Check interface {
	Name() string
	Check(ctx context.Context) error
}

// CheckFunc adapts a name + function pair into a Check.
type CheckFunc struct {
	name string
	fn   func(ctx context.Context) error
}

func (c *CheckFunc) Name() string               { return c.name }
func (c *CheckFunc) Check(ctx context.Context) error { return c.fn(ctx) }

// NewCheck creates a Check from a name and function.
func NewCheck(name string, fn func(ctx context.Context) error) Check {
	return &CheckFunc{name: name, fn: fn}
}

// PostgresCheck verifies the database is reachable with PingContext.
func PostgresCheck(db *sql.DB) Check {
	return NewCheck("postgres", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return db.PingContext(ctx)
	})
}

// HTTPCheck verifies any HTTP endpoint is reachable with a GET request.
func HTTPCheck(name, url string) Check {
	return NewCheck(name, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		return &httpCheckError{status: resp.StatusCode}
	})
}

type httpCheckError struct {
	status int
}

func (e *httpCheckError) Error() string {
	return fmt.Sprintf("HTTP %d", e.status)
}

// DragonflyCheck and RabbitMQCheck are not included to avoid pulling redis/amqp
// dependencies into shared-go. Consumers can implement them using NewCheck:
//
//	// DragonflyCheck example (requires github.com/redis/go-redis/v9):
//	func DragonflyCheck(addr string) health.Check {
//	    return health.NewCheck("dragonfly", func(ctx context.Context) error {
//	        client := redis.NewClient(&redis.Options{Addr: addr})
//	        return client.Ping(ctx).Err()
//	    })
//	}
//
//	// RabbitMQCheck example (requires github.com/rabbitmq/amqp091-go):
//	func RabbitMQCheck(url string) health.Check {
//	    return health.NewCheck("rabbitmq", func(ctx context.Context) error {
//	        conn, err := amqp.Dial(url)
//	        if err != nil { return err }
//	        conn.Close()
//	        return nil
//	    })
//	}
