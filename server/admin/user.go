package admin

import (
	"database/sql"
	"fmt"
	"net/http"

	"git.nkagami.me/natsukagami/kjudge/db"
	"git.nkagami.me/natsukagami/kjudge/models"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
)

// UserCtx is a context for rendering an user.
type UserCtx struct {
	*models.User

	Submissions *SubmissionsCtx

	EditForm      *UserForm
	EditFormError error
}

// getUser returns the context needed to render an user.
func getUser(db db.DBContext, c echo.Context) (*UserCtx, error) {
	id := c.Param("id")
	u, err := models.GetUser(db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, echo.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &UserCtx{User: u}, nil
}

// Render renders the context.
func (u *UserCtx) Render(db db.DBContext, c echo.Context) error {
	// load the submissions list
	s, err := SubmissionsBy(db, u.User, nil, nil)
	if err != nil {
		return err
	}
	u.Submissions = s
	if u.EditForm == nil {
		u.EditForm = UserToForm(u.User)
		u.EditForm.IsUpdate = true
	}
	status := http.StatusOK
	if u.EditFormError != nil {
		status = http.StatusBadRequest
	}

	return c.Render(status, "admin/user", u)
}

// UserGet implements GET /admin/user/:id
func (g *Group) UserGet(c echo.Context) error {
	ctx, err := getUser(g.db, c)
	if err != nil {
		return err
	}
	return ctx.Render(g.db, c)
}

// UserEdit implements POST /admin/user/:id
func (g *Group) UserEdit(c echo.Context) error {
	tx, err := g.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ctx, err := getUser(tx, c)
	if err != nil {
		return err
	}
	nw := *ctx.User
	var form UserForm
	if err := c.Bind(&form); err != nil {
		return err
	}
	form.IsUpdate = true
	if form.ID != nw.ID {
		return echo.NewHTTPError(http.StatusBadRequest, "cannot change user id")
	}
	if err := form.Bind(&nw); err != nil {
		ctx.EditForm = &form
		ctx.EditFormError = err
		return ctx.Render(tx, c)
	}

	if err := nw.Write(tx); err != nil {
		ctx.EditForm = &form
		ctx.EditFormError = err
		return ctx.Render(tx, c)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/admin/users/%s", nw.ID))
}
