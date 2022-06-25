package planetscale

import (
	"context"
	"encoding/json"
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
	planetscaleTypeName     = "planetscale"
	defaultUserNameTemplate = `{{ printf "v-%s-%s-%s-%s" (.DisplayName | truncate 8) (.RoleName | truncate 8) (random 20 | lowercase) (unix_time) | truncate 63 }}`
)

var (
	_ dbplugin.Database = &Planetscale{}
)

type planetscaleDBSatement struct {
	Branch string `json:"branch"`
	Role   string `json:"role"`
}

func getDatabaseStatement(commands []string) (planetscaleDBSatement, error) {
	var planetscaleCreationStatement planetscaleDBSatement
	err := json.Unmarshal([]byte(commands[0]), &planetscaleCreationStatement)
	if err != nil {
		return planetscaleCreationStatement, err
	}
	if planetscaleCreationStatement.Branch == "" {
		planetscaleCreationStatement.Branch = "main"
	}
	if planetscaleCreationStatement.Role == "" {
		planetscaleCreationStatement.Role = "admin"
	}

	return planetscaleCreationStatement, nil
}

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

	client, err := p.Connection(ctx)
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to get client: %w", err)
	}

	p.Lock()
	defer p.Unlock()

	planetscaleCreationStatement, err := getDatabaseStatement(req.Statements.Commands)
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("failed to get database statement: %w", err)
	}

	username, err := p.usernameProducer.Generate(req.UsernameConfig)
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("failed to generate username: %w", err)
	}

	createRequest := planetscale.DatabaseBranchPasswordRequest{
		Organization: p.Organization,
		Database:     p.Database,
		Branch:       planetscaleCreationStatement.Branch,
		DisplayName:  username,
		Role:         planetscaleCreationStatement.Role,
	}

	// TODO: figure out if we can use planetscale expiration
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
	client, err := p.Connection(ctx)
	if err != nil {
		return dbplugin.DeleteUserResponse{}, err
	}

	p.Lock()
	defer p.Unlock()

	passwords, err := client.Passwords.List(ctx, &planetscale.ListDatabaseBranchPasswordRequest{
		Database:     p.Database,
		Organization: p.Organization,
	})
	if err != nil {
		return dbplugin.DeleteUserResponse{}, fmt.Errorf("failed to list existing passwords: %w", err)
	}

	var passwordToDelete *planetscale.DatabaseBranchPassword
	for _, password := range passwords {
		if password.Name == req.Username {
			passwordToDelete = password
			break
		}
	}

	if passwordToDelete == nil {
		return dbplugin.DeleteUserResponse{}, fmt.Errorf("failed to find password. name: %s, database: %s, organization: %s", req.Username, p.Database, p.Organization)
	}

	err = client.Passwords.Delete(
		ctx, &planetscale.DeleteDatabaseBranchPasswordRequest{
			Database:     p.Database,
			Organization: p.Organization,
			Branch:       passwordToDelete.Branch.Name,
			DisplayName:  req.Username,
			PasswordId:   passwordToDelete.PublicID,
		},
	)

	return dbplugin.DeleteUserResponse{}, err
}

func (p *Planetscale) secretValues() map[string]string {
	return map[string]string{
		p.ServiceToken: "[ServiceToken]",
	}
}
