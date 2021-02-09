/*
	Copyright NetFoundry, Inc.

	Licensed under the Apache License, Version 2.0 (the "License");
	you may not use this file except in compliance with the License.
	You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
	distributed under the License is distributed on an "AS IS" BASIS,
	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
	See the License for the specific language governing permissions and
	limitations under the License.
*/

package routes

import (
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/edge/controller/env"
	"github.com/openziti/edge/controller/model"
	"github.com/openziti/edge/controller/persistence"
	"github.com/openziti/edge/controller/response"
	"github.com/openziti/edge/rest_model"
	"github.com/openziti/fabric/controller/models"
	"github.com/openziti/foundation/util/stringz"
	"github.com/openziti/sdk-golang/ziti"
	"strings"
)

const (
	EntityNameIdentity              = "identities"
	EntityNameIdentityServiceConfig = "service-configs"
)

type PermissionsApi []string

var IdentityLinkFactory = NewIdentityLinkFactory(NewBasicLinkFactory(EntityNameIdentity))

func NewIdentityLinkFactory(selfFactory *BasicLinkFactory) *IdentityLinkFactoryImpl {
	return &IdentityLinkFactoryImpl{
		BasicLinkFactory: *selfFactory,
	}
}

type IdentityLinkFactoryImpl struct {
	BasicLinkFactory
}

func (factory *IdentityLinkFactoryImpl) Links(entity models.Entity) rest_model.Links {
	links := factory.BasicLinkFactory.Links(entity)
	links[EntityNameEdgeRouterPolicy] = factory.NewNestedLink(entity, EntityNameEdgeRouter)
	links[EntityNameServicePolicy] = factory.NewNestedLink(entity, EntityNameServicePolicy)
	links[EntityNamePostureData] = factory.NewNestedLink(entity, EntityNamePostureData)

	return links
}

func getTerminatorCost(v *rest_model.TerminatorCost) uint16 {
	if v == nil {
		return 0
	}
	return uint16(*v)
}

func MapCreateIdentityToModel(identity *rest_model.IdentityCreate, identityTypeId string) (*model.Identity, []*model.Enrollment) {
	var enrollments []*model.Enrollment

	ret := &model.Identity{
		BaseEntity: models.BaseEntity{
			Tags: identity.Tags,
		},
		Name:                     stringz.OrEmpty(identity.Name),
		IdentityTypeId:           identityTypeId,
		IsDefaultAdmin:           false,
		IsAdmin:                  *identity.IsAdmin,
		RoleAttributes:           identity.RoleAttributes,
		DefaultHostingPrecedence: ziti.GetPrecedenceForLabel(string(identity.DefaultHostingPrecedence)),
		DefaultHostingCost:       getTerminatorCost(identity.DefaultHostingCost),
	}

	if identity.Enrollment != nil {
		if identity.Enrollment.Ott {
			enrollments = append(enrollments, &model.Enrollment{
				BaseEntity: models.BaseEntity{},
				Method:     persistence.MethodEnrollOtt,
				Token:      uuid.New().String(),
			})
		} else if identity.Enrollment.Ottca != "" {
			caId := identity.Enrollment.Ottca
			enrollments = append(enrollments, &model.Enrollment{
				BaseEntity: models.BaseEntity{},
				Method:     persistence.MethodEnrollOttCa,
				Token:      uuid.New().String(),
				CaId:       &caId,
			})
		} else if identity.Enrollment.Updb != "" {
			username := identity.Enrollment.Updb
			enrollments = append(enrollments, &model.Enrollment{
				BaseEntity: models.BaseEntity{},
				Method:     persistence.MethodEnrollUpdb,
				Token:      uuid.New().String(),
				Username:   &username,
			})
		}
	}

	return ret, enrollments
}

func MapUpdateIdentityToModel(id string, identity *rest_model.IdentityUpdate, identityTypeId string) *model.Identity {
	ret := &model.Identity{
		BaseEntity: models.BaseEntity{
			Tags: identity.Tags,
			Id:   id,
		},
		Name:                     stringz.OrEmpty(identity.Name),
		IdentityTypeId:           identityTypeId,
		IsAdmin:                  *identity.IsAdmin,
		RoleAttributes:           identity.RoleAttributes,
		DefaultHostingPrecedence: ziti.GetPrecedenceForLabel(string(identity.DefaultHostingPrecedence)),
		DefaultHostingCost:       getTerminatorCost(identity.DefaultHostingCost),
	}

	return ret
}

func MapPatchIdentityToModel(id string, identity *rest_model.IdentityPatch, identityTypeId string) *model.Identity {
	ret := &model.Identity{
		BaseEntity: models.BaseEntity{
			Tags: identity.Tags,
			Id:   id,
		},
		Name:                     identity.Name,
		IdentityTypeId:           identityTypeId,
		IsAdmin:                  identity.IsAdmin,
		RoleAttributes:           identity.RoleAttributes,
		DefaultHostingPrecedence: ziti.GetPrecedenceForLabel(string(identity.DefaultHostingPrecedence)),
		DefaultHostingCost:       getTerminatorCost(identity.DefaultHostingCost),
	}

	return ret
}

func MapIdentityToRestEntity(ae *env.AppEnv, _ *response.RequestContext, e models.Entity) (interface{}, error) {
	identity, ok := e.(*model.Identity)

	if !ok {
		err := fmt.Errorf("entity is not a Identity \"%s\"", e.GetId())
		log := pfxlog.Logger()
		log.Error(err)
		return nil, err
	}

	restModel, err := MapIdentityToRestModel(ae, identity)

	if err != nil {
		err := fmt.Errorf("could not convert to API entity \"%s\": %s", e.GetId(), err)
		log := pfxlog.Logger()
		log.Error(err)
		return nil, err
	}
	return restModel, nil
}

func MapIdentityToRestModel(ae *env.AppEnv, identity *model.Identity) (*rest_model.IdentityDetail, error) {

	identityType, err := ae.Handlers.IdentityType.ReadByIdOrName(identity.IdentityTypeId)

	if err != nil {
		return nil, err
	}

	mfa, err := ae.Handlers.Mfa.ReadByIdentityId(identity.Id)

	isMfaEnabled := mfa != nil && mfa.IsVerified

	hasApiSession := false

	if apiSessionList, err := ae.GetHandlers().ApiSession.Query(fmt.Sprintf(`identity = "%s" limit 1`, identity.Id)); err == nil {
		hasApiSession = apiSessionList.Count > 0
	} else {
		pfxlog.Logger().Errorf("error attempting to determine identity id's [%s] API session existence: %v", identity.Id, err)
	}

	defaultCost := rest_model.TerminatorCost(identity.DefaultHostingCost)

	ret := &rest_model.IdentityDetail{
		BaseEntity:               BaseEntityToRestModel(identity, IdentityLinkFactory),
		IsAdmin:                  &identity.IsAdmin,
		IsDefaultAdmin:           &identity.IsDefaultAdmin,
		Name:                     &identity.Name,
		RoleAttributes:           identity.RoleAttributes,
		Type:                     ToEntityRef(identityType.Name, identityType, IdentityTypeLinkFactory),
		TypeID:                   &identityType.Id,
		HasEdgeRouterConnection:  &identity.HasHeartbeat,
		HasAPISession:            &hasApiSession,
		DefaultHostingPrecedence: rest_model.TerminatorPrecedence(identity.DefaultHostingPrecedence.String()),
		DefaultHostingCost:       &defaultCost,
		IsMfaEnabled:             &isMfaEnabled,
	}
	fillInfo(ret, identity.EnvInfo, identity.SdkInfo)

	ret.Authenticators = &rest_model.IdentityAuthenticators{}
	if err = ae.GetHandlers().Identity.CollectAuthenticators(identity.Id, func(entity *model.Authenticator) error {
		if entity.Method == persistence.MethodAuthenticatorUpdb {
			ret.Authenticators.Updb = &rest_model.IdentityAuthenticatorsUpdb{
				Username: entity.ToUpdb().Username,
			}
		}

		if entity.Method == persistence.MethodAuthenticatorCert {
			ret.Authenticators.Cert = &rest_model.IdentityAuthenticatorsCert{
				Fingerprint: entity.ToCert().Fingerprint,
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	ret.Enrollment = &rest_model.IdentityEnrollments{}
	if err := ae.GetHandlers().Identity.CollectEnrollments(identity.Id, func(entity *model.Enrollment) error {
		var expiresAt strfmt.DateTime
		if entity.ExpiresAt != nil {
			expiresAt = strfmt.DateTime(*entity.ExpiresAt)
		}

		if entity.Method == persistence.MethodEnrollUpdb {

			ret.Enrollment.Updb = &rest_model.IdentityEnrollmentsUpdb{
				ID:        entity.Id,
				Jwt:       entity.Jwt,
				Token:     entity.Token,
				ExpiresAt: expiresAt,
			}
		}

		if entity.Method == persistence.MethodEnrollOtt {
			ret.Enrollment.Ott = &rest_model.IdentityEnrollmentsOtt{
				ID:        entity.Id,
				Jwt:       entity.Jwt,
				Token:     entity.Token,
				ExpiresAt: expiresAt,
			}
		}

		if entity.Method == persistence.MethodEnrollOttCa {
			if ca, err := ae.Handlers.Ca.Read(*entity.CaId); err == nil {
				ret.Enrollment.Ottca = &rest_model.IdentityEnrollmentsOttca{
					ID:        entity.Id,
					Ca:        ToEntityRef(ca.Name, ca, CaLinkFactory),
					CaID:      ca.Id,
					Jwt:       entity.Jwt,
					Token:     entity.Token,
					ExpiresAt: expiresAt,
				}
			} else {
				pfxlog.Logger().Errorf("could not read CA [%s] to render ottca enrollment for identity [%s]", stringz.OrEmpty(entity.CaId), identity.Id)
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func fillInfo(identity *rest_model.IdentityDetail, envInfo *model.EnvInfo, sdkInfo *model.SdkInfo) {
	if envInfo != nil {
		identity.EnvInfo = &rest_model.EnvInfo{
			Arch:      envInfo.Arch,
			Os:        envInfo.Os,
			OsRelease: envInfo.OsRelease,
			OsVersion: envInfo.OsVersion,
		}
	} else {
		identity.EnvInfo = &rest_model.EnvInfo{}
	}

	if sdkInfo != nil {
		identity.SdkInfo = &rest_model.SdkInfo{
			AppID:      sdkInfo.AppId,
			AppVersion: sdkInfo.AppVersion,
			Branch:     sdkInfo.Branch,
			Revision:   sdkInfo.Revision,
			Type:       sdkInfo.Type,
			Version:    sdkInfo.Version,
		}
	} else {
		identity.SdkInfo = &rest_model.SdkInfo{}
	}
}

func MapServiceConfigToModel(config rest_model.ServiceConfigAssign) model.ServiceConfig {
	return model.ServiceConfig{
		Service: stringz.OrEmpty(config.ServiceID),
		Config:  stringz.OrEmpty(config.ConfigID),
	}
}
func MapAdvisorServiceReachabilityToRestEntity(entity *model.AdvisorServiceReachability) *rest_model.PolicyAdvice {

	var commonRouters []*rest_model.RouterEntityRef

	for _, router := range entity.CommonRouters {
		commonRouters = append(commonRouters, &rest_model.RouterEntityRef{
			EntityRef: *ToEntityRef(router.Router.Name, router.Router, EdgeRouterLinkFactory),
			IsOnline:  &router.IsOnline,
		})
	}

	result := &rest_model.PolicyAdvice{
		IdentityID:          entity.Identity.Id,
		Identity:            ToEntityRef(entity.Identity.Name, entity.Identity, IdentityLinkFactory),
		ServiceID:           entity.Service.Id,
		Service:             ToEntityRef(entity.Service.Name, entity.Service, ServiceLinkFactory),
		IsBindAllowed:       entity.IsBindAllowed,
		IsDialAllowed:       entity.IsDialAllowed,
		IdentityRouterCount: int32(entity.IdentityRouterCount),
		ServiceRouterCount:  int32(entity.ServiceRouterCount),
		CommonRouters:       commonRouters,
	}

	return result
}

func GetNamedIdentityRoles(identityHandler *model.IdentityHandler, roles []string) rest_model.NamedRoles {
	result := rest_model.NamedRoles{}
	for _, role := range roles {
		if strings.HasPrefix(role, "@") {

			identity, err := identityHandler.Read(role[1:])
			if err != nil {
				pfxlog.Logger().Errorf("error converting identity role [%s] to a named role: %v", role, err)
				continue
			}

			result = append(result, &rest_model.NamedRole{
				Role: role,
				Name: "@" + identity.Name,
			})
		} else {
			result = append(result, &rest_model.NamedRole{
				Role: role,
				Name: role,
			})
		}

	}
	return result
}
