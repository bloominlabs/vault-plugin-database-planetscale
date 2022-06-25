package planetscale

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/go-secure-stdlib/strutil"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
	"github.com/hashicorp/vault/sdk/database/helper/dbutil"
	"github.com/hashicorp/vault/sdk/helper/template"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/planetscale/planetscale-go/planetscale"
)

const (
	planetscaleTypeName        = "pgx"
	defaultExpirationStatement = `
ALTER ROLE "{{name}}" VALID UNTIL '{{expiration}}';
`
	defaultChangePasswordStatement = `
ALTER ROLE "{{username}}" WITH PASSWORD '{{password}}';
`

	expirationFormat = "2006-01-02 15:04:05-0700"

	defaultUserNameTemplate = `{{ printf "v-%s-%s-%s-%s" (.DisplayName | truncate 8) (.RoleName | truncate 8) (random 20) (unix_time) | truncate 63 }}`
)

var (
	_ dbplugin.Database = &Planetscale{}
)

func New() (interface{}, error) {
	db := new()
	// Wrap the plugin with middleware to sanitize errors
	dbType := dbplugin.NewDatabaseErrorSanitizerMiddleware(db, db.secretValues)
	return dbType, nil
}

func new() *Planetscale {
	connProducer := &planetscaleConnectionProducer{}
	connProducer.Type = planetscaleTypeName

	db := &Planetscale{
		planetscaleConnectionProducer: connProducer,
	}

	return db
}

type Planetscale struct {
	*planetscaleConnectionProducer
	usernameProducer template.StringTemplate
}

func (p *Planetscale) Initialize(ctx context.Context, req dbplugin.InitializeRequest) (dbplugin.InitializeResponse, error) {
	newConf, err := p.planetscaleConnectionProducer.Init(ctx, req.Config, req.VerifyConnection)
	if err != nil {
		return dbplugin.InitializeResponse{}, err
	}

	usernameTemplate, err := strutil.GetString(req.Config, "username_template")
	if err != nil {
		return dbplugin.InitializeResponse{}, fmt.Errorf("failed to retrieve username_template: %w", err)
	}
	if usernameTemplate == "" {
		usernameTemplate = defaultUserNameTemplate
	}

	up, err := template.NewTemplate(template.Template(usernameTemplate))
	if err != nil {
		return dbplugin.InitializeResponse{}, fmt.Errorf("unable to initialize username template: %w", err)
	}
	p.usernameProducer = up

	_, err = p.usernameProducer.Generate(dbplugin.UsernameMetadata{})
	if err != nil {
		return dbplugin.InitializeResponse{}, fmt.Errorf("invalid username template: %w", err)
	}

	resp := dbplugin.InitializeResponse{
		Config: newConf,
	}
	return resp, nil
}

func (p *Planetscale) Type() (string, error) {
	return planetscaleTypeName, nil
}

func (p *Planetscale) getClient(ctx context.Context) (*planetscale.Client, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (p *Planetscale) UpdateUser(ctx context.Context, req dbplugin.UpdateUserRequest) (dbplugin.UpdateUserResponse, error) {
	if req.Username == "" {
		return dbplugin.UpdateUserResponse{}, fmt.Errorf("missing username")
	}
	if req.Password == nil && req.Expiration == nil {
		return dbplugin.UpdateUserResponse{}, fmt.Errorf("no changes requested")
	}

	merr := &multierror.Error{}
	if req.Password != nil {
		err := p.changeUserPassword(ctx, req.Username, req.Password)
		merr = multierror.Append(merr, err)
	}
	if req.Expiration != nil {
		err := p.changeUserExpiration(ctx, req.Username, req.Expiration)
		merr = multierror.Append(merr, err)
	}
	return dbplugin.UpdateUserResponse{}, merr.ErrorOrNil()
}

// unimplemented
func (p *Planetscale) changeUserPassword(ctx context.Context, username string, changePass *dbplugin.ChangePassword) error {
	return nil
}

func (p *Planetscale) changeUserExpiration(ctx context.Context, username string, changeExp *dbplugin.ChangeExpiration) error {
	return nil
}

func (p *Planetscale) NewUser(ctx context.Context, req dbplugin.NewUserRequest) (dbplugin.NewUserResponse, error) {
	if len(req.Statements.Commands) == 0 {
		return dbplugin.NewUserResponse{}, dbutil.ErrEmptyCreationStatement
	}

	p.Lock()
	defer p.Unlock()

	username, err := p.usernameProducer.Generate(req.UsernameConfig)
	if err != nil {
		return dbplugin.NewUserResponse{}, err
	}

	client, err := p.getClient(ctx)
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to get client: %w", err)
	}

	createRequest := planetscale.DatabaseBranchPasswordRequest{
		Organization: "bloominlabs",
		Database:     "bloominlabs",
		Branch:       "main",
		DisplayName:  username,
		Role:         req.UsernameConfig.RoleName,
	}
	_, err = client.Passwords.Create(ctx, &createRequest)
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to create password: %w", err)
	}

	resp := dbplugin.NewUserResponse{
		Username: username,
	}
	return resp, nil
}

func (p *Planetscale) DeleteUser(ctx context.Context, req dbplugin.DeleteUserRequest) (dbplugin.DeleteUserResponse, error) {
	p.Lock()
	defer p.Unlock()

	if len(req.Statements.Commands) == 0 {
		return dbplugin.DeleteUserResponse{}, p.defaultDeleteUser(ctx, req.Username)
	}

	return dbplugin.DeleteUserResponse{}, p.customDeleteUser(ctx, req.Username, req.Statements.Commands)
}

func (p *Planetscale) customDeleteUser(ctx context.Context, username string, revocationStmts []string) error {
	client, err := p.getClient(ctx)
	if err != nil {
		return err
	}

	err = client.Passwords.Delete(
		ctx, &planetscale.DeleteDatabaseBranchPasswordRequest{
			Database:     "bloominlabs",
			Organization: "bloominlabs",
			Branch:       "main",
			DisplayName:  username,
		},
	)

	return err
}

func (p *Planetscale) defaultDeleteUser(ctx context.Context, username string) error {
	client, err := p.getClient(ctx)
	if err != nil {
		return err
	}

	err = client.Passwords.Delete(
		ctx, &planetscale.DeleteDatabaseBranchPasswordRequest{
			Database:     "bloominlabs",
			Organization: "bloominlabs",
			Branch:       "main",
			DisplayName:  username,
		},
	)

	return err
}

func (p *Planetscale) secretValues() map[string]string {
	return map[string]string{
		p.Password: "[password]",
	}
}
