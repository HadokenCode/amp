package service

import (
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"

	"docker.io/go-docker/api/types"
	"docker.io/go-docker/api/types/filters"
	"github.com/appcelerator/amp/data/accounts"
	"github.com/appcelerator/amp/data/stacks"
	"github.com/appcelerator/amp/pkg/docker"
	"github.com/golang/protobuf/ptypes/empty"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server is used to implement ServiceServer
type Server struct {
	Accounts accounts.Interface
	Docker   *docker.Docker
	Stacks   stacks.Interface
}

// Service constants
const (
	RoleLabel          = "io.amp.role"
	LatestTag          = "latest"
	GlobalMode         = "global"
	ReplicatedMode     = "replicated"
	StackNameLabelName = "com.docker.stack.namespace"
)

// Tasks implements service.Containers
func (s *Server) Tasks(ctx context.Context, in *TasksRequest) (*TasksReply, error) {
	log.Infoln("[service] Tasks", in.ServiceId)
	args := filters.NewArgs()
	args.Add("service", in.ServiceId)
	list, err := s.Docker.TaskList(ctx, types.TaskListOptions{Filters: args})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	taskList := &TasksReply{}
	for _, item := range list {
		task := &Task{
			Id:           item.ID,
			Image:        strings.Split(item.Spec.ContainerSpec.Image, "@")[0],
			CurrentState: strings.ToUpper(string(item.Status.State)),
			DesiredState: strings.ToUpper(string(item.DesiredState)),
			NodeId:       item.NodeID,
			Error:        item.Status.Err,
		}
		taskList.Tasks = append(taskList.Tasks, task)
	}
	return taskList, nil
}

// ListService implements service.ListService
func (s *Server) ListService(ctx context.Context, in *ServiceListRequest) (*ServiceListReply, error) {
	log.Infoln("[service] List ", in.StackName)
	serviceList, err := s.Docker.ServicesList(ctx, types.ServiceListOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	reply := &ServiceListReply{}
	for _, service := range serviceList {
		if _, ok := service.Spec.Labels[RoleLabel]; ok {
			continue // ignore amp infrastructure services
		}
		stackName := service.Spec.Labels[StackNameLabelName]
		if in.StackName != "" && stackName != in.StackName {
			continue // filter based on provided stack name
		}
		entity := &ServiceEntity{
			Id:   service.ID,
			Name: service.Spec.Name,
		}
		image := service.Spec.TaskTemplate.ContainerSpec.Image
		if strings.Contains(image, "@") {
			image = strings.Split(image, "@")[0] // trimming the hash
		}
		entity.Image = image
		entity.Tag = LatestTag
		if strings.Contains(image, ":") {
			index := strings.LastIndex(image, ":")
			entity.Image = image[:index]
			entity.Tag = image[index+1:]
		}
		entity.Mode = ReplicatedMode
		if service.Spec.Mode.Global != nil {
			entity.Mode = GlobalMode
		}
		response, err := s.serviceStatusReplicas(ctx, entity)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		reply.Entries = append(reply.Entries, response)
	}
	return reply, nil
}

func (s *Server) serviceStatusReplicas(ctx context.Context, service *ServiceEntity) (*ServiceListEntry, error) {
	statusReplicas, err := s.Docker.ServiceStatus(ctx, service.Id)
	if err != nil {
		return nil, err
	}
	return &ServiceListEntry{Service: service, ReadyTasks: statusReplicas.RunningTasks, TotalTasks: statusReplicas.TotalTasks, FailedTasks: statusReplicas.FailedTasks, Status: statusReplicas.Status}, nil
}

// InspectService inspects a service
func (s *Server) InspectService(ctx context.Context, in *ServiceInspectRequest) (*ServiceInspectReply, error) {
	log.Infoln("[service] Inspect", in.ServiceId)
	serviceEntity, err := s.Docker.ServiceInspect(ctx, in.ServiceId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	entity, _ := json.MarshalIndent(serviceEntity, "", "	")
	return &ServiceInspectReply{ServiceEntity: string(entity)}, nil
}

// ScaleService scales a service
func (s *Server) ScaleService(ctx context.Context, in *ServiceScaleRequest) (*empty.Empty, error) {
	log.Infoln("[service] Scale", in.ServiceId)
	serviceEntity, err := s.Docker.ServiceInspect(ctx, in.ServiceId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	stackName := serviceEntity.Spec.Labels[StackNameLabelName]

	stack, dockerErr := s.Stacks.GetByFragmentOrName(ctx, stackName)
	if dockerErr != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if stack == nil {
		return nil, stacks.NotFound
	}

	// Check authorization
	if !s.Accounts.IsAuthorized(ctx, stack.Owner, accounts.UpdateAction, accounts.StackRN, stack.Id) {
		return nil, status.Errorf(codes.PermissionDenied, "user not authorized")
	}

	if err := s.Docker.ServiceScale(ctx, in.ServiceId, in.ReplicasNumber); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &empty.Empty{}, nil
}
