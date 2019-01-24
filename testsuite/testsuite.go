package testsuite

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/gomodule/redigo/redis"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

// Reset clears out both our database and redis DB
func Reset() (context.Context, *sqlx.DB, *redis.Pool) {
	logrus.SetLevel(logrus.DebugLevel)
	ResetDB()
	ResetRP()

	return CTX(), DB(), RP()
}

// ResetDB resets our database to our base state from our RapidPro dump
//
// mailroom_test.dump can be regenerated by running:
//   % python manage.py mailroom_db
//
// then copying the mailroom_test.dump file to your mailroom root directory
//   % cp mailroom_test.dump ../mailroom
func ResetDB() {
	db := sqlx.MustOpen("postgres", "postgres://mailroom_test:temba@localhost/mailroom_test?sslmode=disable")
	defer db.Close()
	db.MustExec("drop owned by mailroom_test cascade")
	dir, _ := os.Getwd()

	// our working directory is set to the directory of the module being tested, we want to get just
	// the portion that points to the mailroom directory
	for !strings.HasSuffix(dir, "mailroom") && dir != "/" {
		dir = path.Dir(dir)
	}

	mustExec("pg_restore", "-d", "mailroom_test", path.Join(dir, "./mailroom_test.dump"))
}

// DB returns an open test database pool
func DB() *sqlx.DB {
	db := sqlx.MustOpen("postgres", "postgres://mailroom_test:temba@localhost/mailroom_test?sslmode=disable")
	return db
}

// ResetRP resets our redis database
func ResetRP() {
	rc, err := redis.Dial("tcp", "localhost:6379")
	if err != nil {
		panic(fmt.Sprintf("error connecting to redis db: %s", err.Error()))
	}
	rc.Do("SELECT", 0)
	_, err = rc.Do("FLUSHDB")
	if err != nil {
		panic(fmt.Sprintf("error flushing redis db: %s", err.Error()))
	}
}

// RP returns a redis pool to our test database
func RP() *redis.Pool {
	return &redis.Pool{
		Dial: func() (redis.Conn, error) {
			conn, err := redis.Dial("tcp", "localhost:6379")
			if err != nil {
				return nil, err
			}
			_, err = conn.Do("SELECT", 0)
			return conn, err
		},
	}
}

// RC returns a redis connection, Close() should be called on it when done
func RC() redis.Conn {
	conn, err := redis.Dial("tcp", "localhost:6379")
	if err != nil {
		panic(err)
	}
	_, err = conn.Do("SELECT", 0)
	if err != nil {
		panic(err)
	}
	return conn
}

// CTX returns our background testing context
func CTX() context.Context {
	return context.Background()
}

// utility function for running a command panicking if there is any error
func mustExec(command string, args ...string) {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("error restoring database: %s: %s", err, string(output)))
	}
}

// AssertQueryCount can be used to assert that a query returns the expected number of
func AssertQueryCount(t *testing.T, db *sqlx.DB, sql string, args []interface{}, count int, errMsg ...interface{}) {
	var c int
	err := db.Get(&c, sql, args...)
	assert.NoError(t, err)
	assert.Equal(t, count, c, errMsg...)
}
