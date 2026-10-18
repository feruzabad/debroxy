// Package command implements the job commands.
package command

import (
	"errors"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

func toDomainActor(actor in.Actor) (remoteitem.Actor, error) {
	role, err := remoteitem.NewRoleFromString(actor.Role)
	if err != nil {
		return remoteitem.Actor{}, err
	}
	if actor.UserID == "" {
		return remoteitem.Actor{}, errors.New("actor without user ID")
	}
	return remoteitem.Actor{UserID: actor.UserID, Role: role}, nil
}

func toDomainSource(source in.Source) (remoteitem.Source, error) {
	sourceType, err := remoteitem.NewSourceTypeFromString(source.Type)
	if err != nil {
		return remoteitem.Source{}, remoteitem.InvalidSourceError{Reason: err.Error()}
	}
	switch sourceType {
	case remoteitem.SourceMagnet:
		return remoteitem.NewMagnetSource(source.Magnet), nil
	case remoteitem.SourceTorrentFile:
		return remoteitem.NewTorrentFileSource(source.File, source.FileName), nil
	case remoteitem.SourceNZBFile:
		return remoteitem.NewNZBFileSource(source.File, source.FileName), nil
	case remoteitem.SourceNZBURL:
		return remoteitem.NewNZBURLSource(source.URL), nil
	}
	return remoteitem.NewWebURLSource(source.URL, source.Password), nil
}
