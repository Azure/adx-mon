package kustainer

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type KustainerContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (*KustainerContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        img,
		ExposedPorts: []string{"8080/tcp"},
		Env: map[string]string{
			"ACCEPT_EULA": "Y",
		},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("8080/tcp"),
			wait.ForLog(".*Hit 'CTRL-C' or 'CTRL-BREAK' to quit.*").AsRegexp(),
		),
	}
	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	}

	for _, opt := range opts {
		if err := opt.Customize(&genericContainerReq); err != nil {
			return nil, err
		}
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	var c *KustainerContainer
	if container != nil {
		c = &KustainerContainer{Container: container}
	}

	if err != nil {
		return c, fmt.Errorf("generic container: %w", err)
	}

	return c, nil
}

func WithNetwork(networkName string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Networks = []string{networkName}
		return nil
	}
}

func (c *KustainerContainer) MustConnectionUrl(ctx context.Context) string {
	connectionString, err := c.ConnectionUrl(ctx)
	if err != nil {
		panic(err)
	}
	return connectionString
}

func (c *KustainerContainer) ConnectionUrl(ctx context.Context) (string, error) {
	containerPort, err := c.MappedPort(ctx, "8080/tcp")
	if err != nil {
		return "", err
	}

	host, err := c.Host(ctx)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("http://%s:%s", host, containerPort.Port()), nil
}

func (c *KustainerContainer) Service(ctx context.Context) (*corev1.Service, error) {
	containerPort, err := c.MappedPort(ctx, "8080/tcp")
	if err != nil {
		return nil, err
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kustainer",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				"app": "kustainer",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "kustainer",
					Protocol:   corev1.ProtocolTCP,
					NodePort:   int32(containerPort.Int()),
					TargetPort: intstr.FromInt(8080),
					Port:       8080,
				},
			},
		},
	}, nil
}

func (c *KustainerContainer) CreateDatabase(ctx context.Context, dbName string) error {
	connectionString, err := c.ConnectionUrl(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection string: %w", err)
	}

	cb := kusto.NewConnectionStringBuilder(connectionString)
	client, err := kusto.New(cb)
	if err != nil {
		return fmt.Errorf("new kusto client: %w", err)
	}
	defer client.Close()

	stmt := kql.New(".create database ").AddUnsafe(dbName).AddLiteral(" volatile")
	_, err = client.Mgmt(ctx, "", stmt)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create database %s: %w", dbName, err)
	}

	return nil
}

type TableColum struct {
	Name string
	Type string
}

func (c *KustainerContainer) CreateTable(ctx context.Context, dbName, tblName string, columns []TableColum) error {
	connectionString, err := c.ConnectionUrl(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection string: %w", err)
	}

	cb := kusto.NewConnectionStringBuilder(connectionString)
	client, err := kusto.New(cb)
	if err != nil {
		return fmt.Errorf("new kusto client: %w", err)
	}
	defer client.Close()

	var schema []string
	for _, column := range columns {
		schema = append(schema, fmt.Sprintf("%s:%s", column.Name, column.Type))
	}

	stmt := kql.New(".create table ").AddUnsafe(tblName).AddLiteral(" (").AddUnsafe(strings.Join(schema, ", ")).AddLiteral(")")
	_, err = client.Mgmt(ctx, dbName, stmt)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create table %s: %w", tblName, err)
	}

	return nil
}
