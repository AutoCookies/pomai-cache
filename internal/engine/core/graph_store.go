// File: internal/engine/core/graph_store.go
package core

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AutoCookies/pomai-cache/packages/ds/graph"
)

type GraphStore struct {
	graphs sync.Map
}

type GraphStats struct {
	Name      string
	NodeCount int
	EdgeCount int
}

func NewGraphStore() *GraphStore {
	return &GraphStore{}
}

func (gs *GraphStore) CreateGraph(name string) error {
	if _, exists := gs.graphs.Load(name); exists {
		return fmt.Errorf("graph already exists: %s", name)
	}

	g := graph.NewGraph()
	gs.graphs.Store(name, g)
	return nil
}

func (gs *GraphStore) GetGraph(name string) (*graph.Graph, error) {
	value, exists := gs.graphs.Load(name)
	if !exists {
		return nil, fmt.Errorf("graph not found: %s", name)
	}

	return value.(*graph.Graph), nil
}

func (gs *GraphStore) DropGraph(name string) error {
	gs.graphs.Delete(name)
	return nil
}

func (gs *GraphStore) AddNode(graphName, nodeID string, properties map[string]interface{}) error {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return err
	}

	return g.AddNode(nodeID, properties)
}

func (gs *GraphStore) GetNode(graphName, nodeID string) (*graph.Node, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	node, exists := g.GetNode(nodeID)
	if !exists {
		return nil, fmt.Errorf("node not found:  %s", nodeID)
	}

	return node, nil
}

func (gs *GraphStore) UpdateNode(graphName, nodeID string, properties map[string]interface{}) error {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return err
	}

	return g.UpdateNode(nodeID, properties)
}

func (gs *GraphStore) DeleteNode(graphName, nodeID string) error {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return err
	}

	return g.DeleteNode(nodeID)
}

func (gs *GraphStore) AddEdge(graphName, from, to, edgeType string, weight float64, props map[string]interface{}) error {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return err
	}

	return g.AddEdge(from, to, edgeType, weight, props)
}

func (gs *GraphStore) GetEdge(graphName, from, to string) (*graph.Edge, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	edge, exists := g.GetEdge(from, to)
	if !exists {
		return nil, fmt.Errorf("edge not found: %s -> %s", from, to)
	}

	return edge, nil
}

func (gs *GraphStore) DeleteEdge(graphName, from, to string) error {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return err
	}

	return g.DeleteEdge(from, to)
}

func (gs *GraphStore) GetNeighbors(graphName, nodeID string, depth int, edgeType string) ([]string, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	return g.GetNeighbors(nodeID, depth, edgeType), nil
}

func (gs *GraphStore) ShortestPath(graphName, from, to string, maxDepth int) (*graph.PathResult, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	result := g.ShortestPath(from, to, maxDepth)
	if result == nil {
		return nil, fmt.Errorf("no path found from %s to %s", from, to)
	}

	return result, nil
}

func (gs *GraphStore) PageRank(graphName string, iterations int, dampingFactor float64) (map[string]float64, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	return g.PageRank(iterations, dampingFactor), nil
}

func (gs *GraphStore) DetectCommunities(graphName, algorithm string) (map[string]int, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	return g.DetectCommunities(algorithm), nil
}

func (gs *GraphStore) GetSubgraph(graphName string, nodeIDs []string) (*graph.Graph, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	return g.GetSubgraph(nodeIDs), nil
}

func (gs *GraphStore) ListGraphs() []string {
	names := make([]string, 0)
	gs.graphs.Range(func(key, _ interface{}) bool {
		names = append(names, key.(string))
		return true
	})
	return names
}

func (gs *GraphStore) GraphStats(name string) (*GraphStats, error) {
	g, err := gs.GetGraph(name)
	if err != nil {
		return nil, err
	}

	return &GraphStats{
		Name:      name,
		NodeCount: g.NodeCount(),
		EdgeCount: g.EdgeCount(),
	}, nil
}

func (gs *GraphStore) ExportGraph(graphName string) ([]byte, error) {
	g, err := gs.GetGraph(graphName)
	if err != nil {
		return nil, err
	}

	data := make(map[string]interface{})
	data["nodes"] = g
	data["edges"] = g

	return json.Marshal(data)
}
