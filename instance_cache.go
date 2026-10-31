package ochazuke

import "sort"

type instanceLink struct {
	instance *Instance
	parent   string
	seq      uint64
}

type instanceIndex struct {
	generation, built uint64
	links             map[string]instanceLink
	children          map[string][]*Instance
}

// Registration/removal changes the generation. Public Parent assignments
// and direct registry writes are supported too: a linear, allocation-free
// check detects them, without sorting unchanged children on every walk.
func (c *instanceIndex) index(r *Runtime) map[string][]*Instance {
	valid := c.children != nil && c.built == c.generation && len(c.links) == len(r.Instances)
	if valid {
		for id, inst := range r.Instances {
			if c.links[id] != (instanceLink{inst, inst.Parent, inst.seq}) {
				valid = false
				break
			}
		}
	}
	if valid {
		return c.children
	}
	c.children = map[string][]*Instance{}
	c.links = make(map[string]instanceLink, len(r.Instances))
	for id, inst := range r.Instances {
		c.links[id] = instanceLink{inst, inst.Parent, inst.seq}
		if inst.Parent != inst.ID {
			c.children[inst.Parent] = append(c.children[inst.Parent], inst)
		}
	}
	for _, kids := range c.children {
		sort.Slice(kids, func(i, j int) bool { return kids[i].seq < kids[j].seq })
	}
	c.built = c.generation
	return c.children
}
