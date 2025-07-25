// Copyright 2018 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package old

import (
	"math"

	"github.com/pingcap/tidb/pkg/planner/cascades/pattern"
	"github.com/pingcap/tidb/pkg/planner/core/operator/physicalop"
	"github.com/pingcap/tidb/pkg/planner/implementation"
	"github.com/pingcap/tidb/pkg/planner/memo"
	"github.com/pingcap/tidb/pkg/planner/property"
	"github.com/pingcap/tidb/pkg/planner/util"
)

// Enforcer defines the interface for enforcer rules.
type Enforcer interface {
	// NewProperty generates relaxed property with the help of enforcer.
	NewProperty(prop *property.PhysicalProperty) (newProp *property.PhysicalProperty)
	// OnEnforce adds physical operators on top of child implementation to satisfy
	// required physical property.
	OnEnforce(reqProp *property.PhysicalProperty, child memo.Implementation) (impl memo.Implementation)
	// GetEnforceCost calculates cost of enforcing required physical property.
	GetEnforceCost(g *memo.Group) float64
}

// GetEnforcerRules gets all candidate enforcer rules based
// on required physical property.
func GetEnforcerRules(g *memo.Group, prop *property.PhysicalProperty) (enforcers []Enforcer) {
	if g.EngineType != pattern.EngineTiDB {
		return
	}
	if !prop.IsSortItemEmpty() {
		enforcers = append(enforcers, orderEnforcer)
	}
	return
}

// OrderEnforcer enforces order property on child implementation.
type OrderEnforcer struct {
}

var orderEnforcer = &OrderEnforcer{}

// NewProperty removes order property from required physical property.
func (*OrderEnforcer) NewProperty(_ *property.PhysicalProperty) (newProp *property.PhysicalProperty) {
	// Order property cannot be empty now.
	newProp = &property.PhysicalProperty{ExpectedCnt: math.MaxFloat64}
	return
}

// OnEnforce adds sort operator to satisfy required order property.
func (*OrderEnforcer) OnEnforce(reqProp *property.PhysicalProperty, child memo.Implementation) (impl memo.Implementation) {
	childPlan := child.GetPlan()
	sort := physicalop.PhysicalSort{
		ByItems: make([]*util.ByItems, 0, len(reqProp.SortItems)),
	}.Init(childPlan.SCtx(), childPlan.StatsInfo(), childPlan.QueryBlockOffset(), &property.PhysicalProperty{ExpectedCnt: math.MaxFloat64})
	for _, item := range reqProp.SortItems {
		item := &util.ByItems{
			Expr: item.Col,
			Desc: item.Desc,
		}
		sort.ByItems = append(sort.ByItems, item)
	}
	impl = implementation.NewSortImpl(sort).AttachChildren(child)
	return
}

// GetEnforceCost calculates cost of sort operator.
func (*OrderEnforcer) GetEnforceCost(g *memo.Group) float64 {
	// We need a SessionCtx to calculate the cost of a sort.
	sctx := g.Equivalents.Front().Value.(*memo.GroupExpr).ExprNode.SCtx()
	sort := physicalop.PhysicalSort{}.Init(sctx, g.Prop.Stats, 0, nil)
	cost := sort.GetCost(g.Prop.Stats.RowCount, g.Prop.Schema)
	return cost
}
