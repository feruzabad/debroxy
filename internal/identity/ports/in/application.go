package in

// Application is everything the identity context can be asked to do.
type Application struct {
	Commands Commands
	Queries  Queries
}

type Commands struct {
	BootstrapAdmin    BootstrapAdminHandler
	CreateUser        CreateUserHandler
	ChangeUserRole    ChangeUserRoleHandler
	DisableUser       DisableUserHandler
	EnableUser        EnableUserHandler
	ResetUserPassword ResetUserPasswordHandler
	ChangeOwnPassword ChangeOwnPasswordHandler
	LogInWithPassword LogInWithPasswordHandler
	LogOut            LogOutHandler
	TouchSession      TouchSessionHandler
	PruneSessions     PruneSessionsHandler

	LinkOIDCIdentity   LinkOIDCIdentityHandler
	UnlinkOIDCIdentity UnlinkOIDCIdentityHandler
	LogInWithOIDC      LogInWithOIDCHandler
}

type Queries struct {
	AuthenticatedActor AuthenticatedActorHandler
	UserStatus         UserStatusHandler
	Users              UsersHandler
	UserDetails        UserDetailsHandler
}
