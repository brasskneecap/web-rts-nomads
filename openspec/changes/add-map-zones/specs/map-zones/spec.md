## ADDED Requirements

### Requirement: Zone schema on `MapConfig`

The system SHALL support a `zones` array on `MapConfig` and its serialised map JSON. Each zone SHALL declare an `id` (unique within the map), an `anchor` grid cell, a `cells` array of grid cells the zone is composed of, a `capture` object whose `type` names a registered capture mechanic and whose remaining fields are that mechanic's config, a `startingOwner` (the `neutral` sentinel or a team/player slot identifier), and an `adjacent` array of zone ids. The `name` field is optional. A map JSON with no `zones` field SHALL load as a zone-free map with unchanged behaviour.

#### Scenario: Map with no zones loads unchanged
- **WHEN** a map JSON omits the `zones` field
- **THEN** the map loads with an empty zone slice and no zone-related behaviour is active

#### Scenario: Authored zone round-trips through the catalog renderer
- **WHEN** a map with a zone is saved by the editor and reloaded from disk
- **THEN** the zone's `id`, `anchor`, `cells`, `capture`, `startingOwner`, and `adjacent` are preserved, with `cells` stored compactly as a list of `[x,y]` pairs

#### Scenario: Duplicate zone id is rejected at load
- **WHEN** a map JSON declares two zones with the same `id`
- **THEN** the catalog loader fails at startup with a message naming the map file and the duplicate id

### Requirement: Single-owner cell membership

Each grid cell SHALL belong to at most one zone. The system SHALL reject at load any map in which two zones list the same cell, naming the map file and the contested cell. The perimeter/interior classification of a zone SHALL be derived from its cell set and SHALL NOT be stored: a member cell with at least one 4-neighbour that is not a member of the same zone is a perimeter cell, and a member cell whose four 4-neighbours are all members of the same zone is an interior cell.

#### Scenario: Overlapping authored zones rejected at load
- **WHEN** a map JSON has zone A and zone B both listing cell `[10,10]`
- **THEN** the catalog loader fails at startup naming the map file and cell `[10,10]`

#### Scenario: Perimeter is derived from membership
- **WHEN** a zone consists of a solid 5x5 block of cells
- **THEN** the 16 outer cells classify as perimeter and the 9 inner cells classify as interior, with no perimeter data stored on the zone

#### Scenario: Single-cell zone is all perimeter
- **WHEN** a zone consists of exactly one cell
- **THEN** that cell classifies as a perimeter cell (it has non-member 4-neighbours)

### Requirement: Adjacency is an authored bidirectional graph

A zone's `adjacent` list SHALL reference other zones by id. The system SHALL validate at load that every id in an `adjacent` list names an existing zone in the same map, and SHALL treat adjacency as symmetric: if zone A lists B as adjacent, B is adjacent to A whether or not B's list mentions A. The loader SHALL reject an `adjacent` entry that names a non-existent zone, naming the map file and the dangling id.

#### Scenario: Symmetric adjacency
- **WHEN** zone A lists `["B"]` as adjacent and zone B lists no adjacency
- **THEN** A and B are treated as mutually adjacent at runtime

#### Scenario: Dangling adjacency reference rejected
- **WHEN** zone A lists `["ghost"]` as adjacent and no zone `ghost` exists
- **THEN** the catalog loader fails at startup naming the map file and the id `ghost`

### Requirement: Zone brush — node placement

The map editor SHALL provide a `zone` brush mode. Clicking an empty cell (one not already owned by any zone) in `zone` mode SHALL create a new zone whose `anchor` is the clicked cell and whose `cells` are a default 5x5 block centred on the anchor (clipped to the map bounds), and SHALL select the new zone. Each created zone SHALL receive a unique generated `id`.

#### Scenario: Stamping creates a 5x5 zone
- **WHEN** the author clicks an empty cell at `[20,20]` in zone mode
- **THEN** a new zone is created with anchor `[20,20]` and the 25 cells from `[18,18]` to `[22,22]`, and that zone becomes the selected zone

#### Scenario: Stamp near the map edge clips to bounds
- **WHEN** the author clicks an empty cell at `[1,1]` in zone mode
- **THEN** the created zone contains only the in-bounds cells of the 5x5 block centred on `[1,1]`

### Requirement: Zone popup — configure capture, owner, and lifecycle actions

Clicking an existing zone's node SHALL open a popup that lets the author edit the zone's `name`, choose its `capture.type` from the registered mechanics and edit that mechanic's fields, and set its `startingOwner`. The popup SHALL expose three actions: **Draw Zone** (enter expand mode for the zone), **Link** (begin an adjacency-link gesture), and **Delete** (remove the zone and drop it from every other zone's `adjacent` list).

#### Scenario: Editing capture type swaps the field set
- **WHEN** the author selects capture type `presence` in a zone's popup
- **THEN** the popup shows the `presence` fields (including `captureSeconds`) and, on selecting `control_point`, shows that mechanic's fields instead

#### Scenario: Deleting a zone cleans up adjacency
- **WHEN** the author deletes a zone that was linked to two other zones
- **THEN** the zone is removed and neither of the two other zones lists the deleted zone in its `adjacent` list

### Requirement: Zone brush — draw/expand mode

Activating **Draw Zone** for the selected zone SHALL enter an expand mode in which a left-click on a cell adds that cell to the selected zone and a right-click on a member cell removes it from the selected zone. Pressing Esc, or re-clicking the zone's node, SHALL exit expand mode. Adding or removing a cell SHALL cause the zone's derived perimeter/interior classification to update.

#### Scenario: Left-click extends the zone by one cell
- **WHEN** the author is in expand mode for a zone and left-clicks an adjacent empty cell
- **THEN** that cell becomes a member of the zone and the perimeter re-derives to include it

#### Scenario: Right-click removes a cell
- **WHEN** the author right-clicks a member cell of the zone in expand mode
- **THEN** that cell is removed from the zone's `cells` and the perimeter re-derives

#### Scenario: Esc exits expand mode
- **WHEN** the author presses Esc while in expand mode
- **THEN** subsequent clicks no longer modify the zone's cells

### Requirement: Zone brush — overlap reassigns to the active zone

When a cell added to the active zone is already owned by another zone, the system SHALL reassign that cell to the active zone (last-writer-wins), removing it from the previous owner's `cells`, and SHALL recompute both zones' derived perimeters. A zone left with zero cells by reassignment MAY remain (as an anchor-only zone) until explicitly deleted.

#### Scenario: Drawing over a neighbour transfers the cell
- **WHEN** cell `[10,10]` belongs to zone A and the author draws it into zone B
- **THEN** `[10,10]` belongs to zone B only, zone A no longer lists it, and both zones' perimeters re-derive

### Requirement: Zone brush — adjacency linking

Activating **Link** on a zone and then clicking another zone's node SHALL toggle a bidirectional adjacency edge between the two zones: if no edge exists it is added to both zones' `adjacent` lists; if an edge already exists it is removed from both. Linking a zone to itself SHALL be a no-op.

#### Scenario: Linking two nodes creates a symmetric edge
- **WHEN** the author activates Link on zone A and clicks zone B's node
- **THEN** A lists B in its `adjacent` and B lists A in its `adjacent`

#### Scenario: Re-linking removes the edge
- **WHEN** zones A and B are already linked and the author Links A then clicks B again
- **THEN** neither zone lists the other in its `adjacent`

### Requirement: Editor zone rendering

The map editor SHALL render zones on the canvas: perimeter cells in a darker grey and interior cells in a lighter shade, with the classification derived each frame from the cell set. Adjacency edges SHALL be drawn as lines connecting linked zones' nodes. The classification and edges SHALL update immediately as the author paints, removes cells, or links zones.

#### Scenario: Perimeter and interior render distinctly
- **WHEN** a zone with both perimeter and interior cells is displayed in the editor
- **THEN** perimeter cells render in the darker grey and interior cells in the lighter shade

#### Scenario: Adjacency renders as connecting lines
- **WHEN** two zones are linked
- **THEN** a line is drawn between their two nodes on the editor canvas
