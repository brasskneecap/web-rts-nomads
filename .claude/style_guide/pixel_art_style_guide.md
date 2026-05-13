# Pixel Art RTS Style Guide

A comprehensive style reference for a Warcraft 2 inspired pixel art real-time strategy game. This document captures all established art direction, prompts, palette decisions, and workflow practices.

---

## Game Overview

**Genre**: Medieval fantasy real-time strategy (RTS)
**Art Style**: Warcraft 2 inspired pixel art with chunky outlines, limited earthy palette, hand-pixeled detail
**Tool**: PixelLab.ai (primary), Aseprite (cleanup and finishing)
**Aesthetic Reference**: Classic 16-bit SNES era RTS with painterly pixel detail influence

---

## Core Art Principles

1. **Chunky dark outlines** on all sprites — defines silhouette and reads at small sizes
2. **Limited earthy palette** — slate blue, cream, warm brown, steel gray as core faction colors
3. **Hand-pixeled look, no anti-aliasing** — clean pixel edges, intentional pixel placement
4. **High top-down 3/4 perspective** for units and buildings
5. **Low top-down (overhead) perspective** for terrain tiles
6. **South-facing** as the standard idle direction
7. **Single subject centered** with transparent background for sprites
8. **Color discipline** — faction colors echo across all assets within a faction
9. **Silhouette test** — every asset should be recognizable by silhouette alone

---

## Faction Palette

### Primary Faction Colors

| Color | Hex Range | Used For |
|-------|-----------|----------|
| Slate Blue | #3D5A7A - #4A6B8C | Banners, tabards, faction accents, town hall roof |
| Cream / Off-White | #E8DDB5 - #F5EBC8 | Heraldic emblems, stone walls, highlights |
| Warm Brown Wood | #6B4226 - #8B5A2B | Poles, frames, leather, civilian gear |
| Steel Gray | #5A5A5A - #8A8A8A | Metal, iron, armor |
| Dark Outline | #1A1A1A - #2C2C2C | Chunky outlines on all assets |
| Warm Yellow Accent | #D4A847 | Gold trim, lamplight, highlights (sparingly used) |

### Enemy Color Discipline

Each enemy type has a distinct dominant color so players read threats instantly:

| Enemy | Palette | Identity |
|-------|---------|----------|
| Kobold | Dingy gray-brown fur | Pest tier (weakest) |
| Goblin | Sickly muted green | Scrapper tier (common) |
| Roc | Dark brown, dusty tan, yellow accents | Flying scrapper tier |
| Vine Monster | Deep forest green + dark brown | Corruption tier |
| Ogre | Deep red, black, dark brown | Elite/boss tier |
| Druidic Faction | Deep forest green + warm brown + cream | Rival faction |

### Effect / Magic Accent Colors

| Effect Type | Palette |
|-------------|---------|
| Fire / Explosion | Warm orange, warm yellow, cream-white core, red, dark gray smoke |
| Wind / Whirlwind | White, pale gray, warm brown dust accents |
| Holy / Heal | Cream, warm yellow, soft glow |
| Frost / Ice | Pale blue, cream-white, sharp crystalline |
| Poison | Sickly green, murky bubbles |
| Voodoo / Dark Magic | Deep purple, violet accents with bone white |

---

## Reusable Style Blocks

These are the prompt boilerplate templates. Prepend the appropriate style block to your asset-specific description.

### Units & Creatures Style Block

```
pixel art, top-down 3/4 perspective, chunky dark outlines, 16-bit SNES era RTS, Warcraft 2 inspired, hand-pixeled look, single character centered on transparent background, idle pose facing south
```

### Buildings Style Block

```
pixel art, top-down 3/4 perspective, chunky dark outlines, limited earthy palette, 16-bit SNES era RTS, Warcraft 2 inspired, hand-pixeled look, no anti-aliasing, clean pixel edges, building centered on transparent background with grass and dirt foundation
```

### Objects Style Block (Props, Items, Decorations)

```
pixel art, top-down 3/4 perspective, chunky dark outlines, limited earthy palette, 16-bit SNES era RTS, Warcraft 2 inspired, hand-pixeled look, no anti-aliasing, clean pixel edges, single object centered on transparent background, small soft shadow at base where object meets ground
```

### Action Icon Style Block (UI Buttons)

```
game UI action icon, single iconic object centered in frame, clean readable silhouette, chunky dark outlines, hand-pixeled detail, no environmental elements, no shadow, flat icon style, Warcraft 2 inspired pixel art style
```

### Effect Overlay Style Block

```
pixel art, top-down 3/4 perspective, chunky dark outlines, 16-bit SNES era RTS, Warcraft 2 inspired, hand-pixeled look, single visual effect on transparent background, designed to overlay on a character sprite
```

### Terrain Tile Style Language

```
hand-pixeled detail, painterly pixel art style, medieval fantasy game terrain, Warcraft 2 inspired pixel art style
```

### Portrait Style Block (UI Character Portraits)

```
pixel art character portrait, head and upper shoulders viewed from front, chunky dark outlines, limited earthy palette, hand-pixeled detail, no anti-aliasing, clean pixel edges, Warcraft 2 inspired pixel art style
```

---

## PixelLab Settings by Asset Type

### Units & Creatures
- **Tool**: Create S-M image (16-64px) or Create M-XL image (64px+)
- **View**: High top-down
- **Direction**: South
- **Size**: 32x32 (tiny) / 48x48 (standard) / 56x56 (medium-small) / 64x64 (medium) / 96x96 (large) / 128x128 (very large) / 168x168+ (boss)
- **Transparent background**: Checked

### Buildings
- **Tool**: Create M-XL image
- **View**: High top-down
- **Direction**: South
- **Size**: 64x64 (small) / 96x96 (medium) / 128x128 (large)
- **Transparent background**: Checked

### Terrain Tiles
- **Tool**: Create tiles (PRO feature)
- **View**: Low top-down (flatter overhead)
- **Size**: 32x32 standard
- **Mode**: Tileset generator with terrain reference images
- **Workflow**: Generate base terrain samples in Create S-M image first, then feed into Create tiles

### Action Icons
- **Tool**: Create S-M image
- **View**: High top-down (matches world assets) or Side/Front for purely iconographic look
- **Direction**: South
- **Size**: 32x32 (standard button size)
- **Transparent background**: Checked

### UI Panels (9-Slice)
- **Tool**: Create UI elements (PRO) or Create M-XL image
- **Size**: 56x56 or 64x64 source asset
- **Transparent background**: Unchecked (panel needs fill)
- **Cleanup required**: Verify uniform border thickness in Aseprite before defining 9-slice insets

### Portraits
- **Tool**: Create S-M image or Create M-XL image
- **View**: Side or Front (face-on, not top-down)
- **Direction**: South
- **Size**: 48x48 (small) / 64x64 (medium) / 96x96 (standard) / 128x128+ (large hero)
- **Transparent background**: Checked or unchecked depending on UI use

### Effects
- **Tool**: Create S-M image
- **View**: High top-down
- **Direction**: South
- **Size**: 48x48-96x96 depending on effect scope
- **Transparent background**: Checked (critical for overlay use)

---

## Unit Roster

### Worker (48x48)
```
peasant laborer, rough brown tunic, leather belt, simple cloth hood, carrying a wooden pickaxe, humble and hunched posture, dirt-stained clothes, no armor, warm brown and tan palette with small slate blue cloth accent
```

### Soldier (48x48)
```
human footman soldier, steel chainmail armor, slate blue tabard with simple cream heraldry, kite shield held on left arm, short sword raised in right hand above shoulder, iron helmet, disciplined military stance, steel gray and slate blue palette accents
```

### Vanguard (48x48)
```
vanguard knight, heavy plate armor, slate blue and silver tabard with bold heraldry, large tower shield, two-handed longsword held vertically, plumed helmet with raised visor, broad imposing stance, ornate armor details, polished steel with slate blue and gold accents
```

### Berserker (48x48)
```
barbarian berserker, bare-chested muscular warrior, brown fur shoulder pauldron, leather kilt, wild long hair and braided beard, large two-handed battle axe raised high, savage aggressive stance, red warpaint on face and chest, no helmet, red and brown palette accents
```

### Ranger / Archer (48x48)
```
ranger archer, forest green hooded cloak, leather jerkin, longbow held loosely at side pointing downward, no arrow nocked, quiver of arrows on back, leather boots, calm watchful stance, earthy greens and browns palette
```

### Trapper (48x48)
```
wilderness trapper hunter, reinforced dark green hooded cloak with fur trim at shoulders, rugged leather armor over cloth tunic, crossbow held at chest, quiver of bolts on back, coiled rope and small iron trap hanging from belt, leather boots with fur lining, small hunting knife sheathed at hip, weathered experienced stance, dark forest green and rugged brown palette
```

### Marksman (48x48)
```
human marksman ranger, elite wilderness archer in relaxed ready stance, hardened brown leather chest armor with white fur trim at collar and shoulders, white linen undershirt visible at sleeves, brown leather bracers and gloves, tall wooden longbow held at side pointing downward, no arrow nocked, quiver of arrows on back, wide brimmed brown leather ranger hat with white feather, sturdy brown leather boots with fur lining at top, belt with arrow pouch and hunting knife, calm watchful posture, warm brown and off-white cream palette
```

### Caster (48x48)
```
human female caster, slender feminine figure in flowing slate blue mage robes with cream trim, ornate cream and slate blue heraldic emblem on chest matching faction soldiers, long dark hair tied back, wooden staff held vertically in right hand topped with glowing cream crystal orb, small spellbook hanging from belt, leather satchel with potion vials at hip, focused mystical stance, faint magical aura at hands, slate blue cloth and steel accents palette
```

### Healer (64x64)
```
human female healer, slender feminine figure in flowing cream and slate blue robed dress, large flowing cloak draped over shoulders with cream interior and warm brown exterior, short hair with small floral hair ornament, holding tall wooden nature staff topped with glowing flower bloom, leather satchel at hip with potion vials, pendant necklace at chest, gentle compassionate stance, soft warm magical aura at staff bloom, cream and slate blue palette with warm brown accents
```

---

## Druidic Faction Roster

### Druidic Caster (48x48)
```
druidic female caster nature mage, slender feminine figure in flowing forest green robed dress with cream linen underlayer, woven leather belt with herb pouches and bone fetishes, cloak of layered green leaves and moss, long auburn hair with flowers and twigs woven into braids, antler circlet on forehead, holding gnarled wooden staff topped with glowing warm yellow flower bloom, small spellbook bound in bark hanging from belt, gentle attuned mystical stance, deep forest green and warm brown palette with cream and warm yellow accents
```

### Druidic Warrior (48x48)
```
druidic female warrior nature guardian, athletic feminine figure in hardened leather and bark armor over forest green tunic, leather pauldrons reinforced with carved wooden plates, woven leaf-patterned cloak, long dark hair tied back in single braid with feathers and bones woven in, war paint streaks across cheeks in earthy red and white, holding curved wooden polearm with leaf-shaped iron blade, small round wooden buckler shield with carved druidic spirals, hunting knife at hip, fierce protective stance, deep forest green and warm brown palette with red and white war paint accents
```

### Druidic Shapeshifter (48x48)
```
druidic female shapeshifter beast warden, athletic feminine figure in hardened leather armor with fur-trimmed accents, large dark brown bear-head cowl with bear's snout extending forward like a hood and hollow eye sockets serving as viewing opening for her face, bear pelt continues down back as flowing cloak with paws at shoulders, forest green tunic visible under bear pelt, leather belt with tooth and claw fetishes, long dark braided hair visible from beneath cowl, war paint streaks under eyes in earthy red, holding heavy wooden quarterstaff with carved druidic spirals and bone-tipped ends, fierce primal stance, deep forest green and dark brown bear fur palette with red war paint accents
```

---

## Enemy / Creature Roster

### Kobold (48x48)
```
rat-like kobold creature, small hunched humanoid, pointed snout, large ears, dingy gray-brown fur, tattered brown loincloth, lit candle on head held by leather strap, small warm green candle flame, drawing wooden shortbow, quiver of arrows on back, stooped cowardly posture, beady black eyes, earthy brown and dirty gray palette
```

### Goblin (56x56)
```
small mischievous goblin creature, scrawny humanoid with sickly muted green skin, large pointed ears, hooked nose with sharp features, sharp jagged teeth in wicked grin, beady yellow eyes, ragged mismatched leather armor scraps, crude rusty iron dagger gripped in right hand, small wooden buckler shield strapped to left arm, leather strap pouch at hip, hunched cunning stance ready to strike, bare clawed feet, wiry muscular frame, dirty green and rusty brown palette
```

### Ogre (96x96, Red/Black Palette)
```
hulking ogre brute, muscular humanoid with warty dark red skin mottled with black patches, broad shoulders, jutting tusks and small beady eyes, blackened leather loincloth and dark hide harness, iron bands on wrists, massive dark wooden club studded with black iron spikes resting on right shoulder, hunched aggressive stance, dark burn scars across chest and arms, deep red and black palette with dark brown accents
```

### Ogre Boss (168x168)
```
[Same description as 96x96 ogre but scaled up with additional detail: scars across chest and arms, thick stumpy legs with worn dark leather wraps]
```

### Roc (48x48, Flying)
```
fierce predatory roc, large raptor-like bird with broad outstretched wings, dark brown and dusty tan plumage with darker streaks, hooked yellow beak open in a screech, sharp golden eyes, massive curved talons extended downward ready to grab prey, feathered head with small tufted crest, wings spread wide showing flight feathers, aggressive aerial predator stance hovering with wings angled forward, scattered loose feathers around the body, dark brown and dusty tan palette with yellow beak and talon accents
```

### Vine Monster — Small Sapling Brute (64x64)
```
small vine monster brute, hulking humanoid silhouette formed from tangled twisted living vines and creeping plant matter, broad shoulders and thick muscular arms made of woven vine clusters, no clearly defined face just dark hollow opening in head where two glowing yellow eye points peer out, small white flowers and budding leaves sprouting from shoulders, gnarled wooden core barely visible through vines, hunched aggressive brute stance, clawed root-fingers, dark forest green and warm brown palette with bright yellow eye accents
```

### Vine Monster — Medium Thicket Brute (96x96)
```
medium vine monster brute, large hulking humanoid mass of densely woven thorny vines and twisted creepers, broad imposing shoulders and powerful arms knotted from layered vine bundles, dark hollow head opening with glowing yellow eye points peering out, sharp black thorns protruding along arms and shoulders, scattered red berries and small leaves growing from torso, thick wooden trunk-like core partially visible, gnarled root system at feet anchoring into the ground, broad menacing brute stance with arms slightly raised, deep forest green and dark brown palette with thorn black and yellow eye accents
```

### Vine Monster — Large Bramble Lord (128x128)
```
large vine monster bramble lord, massive hulking humanoid construct of ancient twisted vines briars and tangled roots forming a brute warrior shape, enormous broad shoulders and tree-trunk arms made of layered vine bundles bound together by thicker woody growths, dark hollow cavern in the head where glowing yellow eyes peer from deep within, prominent black thorns and barbs covering shoulders forearms and chest like natural armor, moss patches and small mushroom growths along the body, ancient gnarled wooden core visible through gaps in the vines, dripping resin or sap from joints, deep root claws at the feet, towering imposing dominant brute stance with massive vine fists clenched, dark forest green and aged brown palette with thorn black and bright yellow eye accents and small red berry highlights
```

### Vine Monster — Ancient Briar King Boss (192x192)
```
ancient briar king vine monster boss, colossal towering humanoid construct of millennia-old vines petrified roots and massive thorny briars formed into a hulking warrior brute, enormous powerful shoulders crowned with antler-like protruding branches reaching upward, massive tree-trunk arms ending in clawed root-hands, deep cavernous head opening with two prominent glowing yellow eyes set deep within darkness, dense dark thorns covering the entire body like spiked armor, weathered moss and lichen growth on shoulders and back, small flowering branches and red berries scattered across the form, ancient mossy wooden chest core visible through layered vines, twisted root system spreading outward from base like a small grove, drooping vine tendrils hanging from arms like cape, imposing dominant kingly brute stance commanding presence, deep dark forest green and weathered brown palette with stark black thorn accents and prominent glowing yellow eyes and small warm red berry highlights
```

---

## Building Roster

All buildings share the architectural language: lower half stone foundation walls with upper half timber and plaster construction (where applicable).

### Town Hall (128x128)
```
human town hall, large stone keep with blue banner flags, wooden reinforced doors, sloped slate roof with tower spires, small windows with warm yellow light, flagpoles flying faction blue pennants, carved wooden beams, moss and weathering on lower stones, imposing central fantasy medieval building
```

### Barracks (96x96)
```
military barracks, lower half stone foundation walls, upper half timber and plaster construction, sloped wooden shingle roof with small chimney, iron-banded wooden door centered on front wall, one round shield mounted above door as decoration, blue banner hanging from short pole beside entrance, small stack of firewood against side wall
```

### Farm (64x64)
```
peasant farm, small wooden cottage with thatched roof, stone chimney with faint smoke, wooden fence enclosing small pen, haystacks beside building, warm earthy brown and tan palette, cozy humble rustic appearance, hanging lantern by door
```

### Tower (64x96, tall vertical)
```
stone guard tower, tall cylindrical or square stone construction, crenellated battlements at top, narrow arrow slits, small blue banner flying from peak, wooden support beams, iron-reinforced base door, slight moss and weathering, imposing defensive fantasy medieval tower
```

### Gold Mine (96x96, neutral structure)
```
fantasy gold mine, large rocky hillside or mound of earth and stone, wooden-framed mine entrance with reinforced timber supports forming an arched doorway into the hill, dark shadowy interior visible through entrance, glints of gold ore and yellow crystals embedded in the surrounding rocks, wooden minecart track leading out of entrance, small wooden minecart beside entrance, piles of rock and gold nuggets scattered at base, wooden pickaxe leaning against rocks, weathered gray stone with brown earth
```

### Blacksmith / Upgrade Center (96x96)
```
blacksmith forge building, lower half stone walls with upper half timber and plaster, sloped wooden shingle roof with tall stone chimney emitting subtle smoke, large open forge entrance with glowing warm orange fire visible inside, iron anvil on wooden block outside the entrance, weapon rack against side wall, slate blue faction banner above entrance, weathered industrious workshop
```

---

## World Objects

### Rally Flag (World Object)
```
tall wooden rally flagpole planted in the ground, long slate blue rectangular banner flying from the top, simple cream heraldic emblem on the banner, gnarled wooden pole with iron base spike driven into earth, small dirt mound at base, banner gently rippling as if in wind, weathered wood and fabric, faction military marker, small shadow at base
```

### Voodoo Staff (Environmental Object, 32x32)
```
voodoo ritual staff planted upright in the ground, gnarled twisted dark wood vertical staff stuck into earth at base, top of staff adorned with bleached animal skull and dangling bone fetishes, small feathers and beaded cords hanging from upper section, faint glowing purple magical aura emanating from the skull, deep purple and violet mystical accents breaking from earthy palette, small cluster of dark purple crystals embedded near the top, weathered bone-white skull with dark eye sockets, base surrounded by small disturbed dirt mound and scattered bones
```

### Tree (48x48, Single)
```
medieval fantasy forest tree, dense round rounded canopy with layered leaf clusters, muted forest green foliage with subtle darker green shading, visible dark brown trunk at base, sturdy wide trunk, small shadow at base
```

### Tree Cluster (64x64)
```
group of three medieval fantasy deciduous trees tightly grouped together as a single forest obstacle, rounded canopies in muted forest green, partial trunks visible at base in dark warm brown, trees slightly different sizes for natural variety, overlapping canopies creating one unified shape, small soft shadow pooled at base
```

### Balsam Fir (48x48)
```
single tall evergreen conifer, classic narrow conical triangular silhouette, layered downward-sloping branches creating tiered tree shape, dense dark blue-green needle foliage with subtle lighter green highlights on upper branches, small visible dark brown trunk at base, small shadow at base
```

### Rock (32x32)
```
weathered gray boulder obstacle, rounded irregular natural shape, subtle moss patches on top edges in muted sage green matching game grass palette, darker gray crevices for depth, lighter gray highlights on upper surface, small shadow at base
```

---

## UI System

### Main Panel (56x56 source, 9-slice scaling)

**Design**: Iron-reinforced dark wood with stone/metal corner brackets, wood grain interior
**Slice Insets**: 16 pixels on all four sides
**Implementation Values**:
- Unity Sprite Editor: L 16, R 16, T 16, B 16
- Godot NinePatchRect: patch_margin all 16
- CSS: `border-image: url('panel.png') 16 fill stretch;`

**Cleanup Required**: Verify uniform border thickness, ensure center wood texture tiles cleanly (consider "tile" mode over "stretch" for wood grain), confirm corners are visually distinct from edges.

### Action Icon Style (32x32)

All action icons follow the action icon style block and share the faction palette. Use the color palette field in PixelLab to lock colors.

**Action Icons Library**:

**Attack**
```
crossed medieval swords forming an X, two blades with cloth-wrapped handles and small jewel pommels, blades catching subtle highlights, classic combat action symbol
```

**Move**
```
single leather boot with footprint trail behind it, sturdy traveling footwear, motion suggested by small dust marks behind heel, classic movement action symbol
```

**Stop / Halt**
```
raised steel armored gauntlet palm forward in halt gesture, slate blue cuff on the gauntlet, cream highlights on metal knuckles, clear stop signal silhouette
```

**Hold Position**
```
single iron anchor stake driven into a small platform with cloth banner ribbon wrapped around the top, sturdy defensive marker, no motion suggested as the symbol implies stillness and rooted defense, classic hold position action symbol
```

**Patrol**
```
circular arrow path made of two curved arrows chasing each other forming a loop, slate blue arrow shafts with cream arrowheads, small wooden post in the center, patrol movement symbol
```

**Guard / Defend**
```
medieval kite shield centered facing forward, slate blue shield face with cream heraldic emblem matching faction banners, steel rim and boss, classic defense symbol
```

**Build**
```
single carpenter hammer with iron head and wooden handle crossed over a stonemason chisel forming an X, classic construction tool combination, motion suggested by small impact spark above the hammer head, classic build action symbol
```

**Repair**
```
single iron wrench tool held vertically with a small repair spark glinting at the top, sturdy industrious tool, motion suggested by small wrench-turning marks at the base, classic repair action symbol
```

**Gather**
```
single woven wicker basket viewed from above with small wooden logs and gold coins overflowing from the top, sturdy resource collection vessel, motion suggested by small upward-floating sparkle marks above the contents, classic gather action symbol
```

**Rally Flag**
```
tall wooden flagpole with slate blue rectangular banner unfurled and flying, cream heraldic emblem centered on the banner, banner extending to one side fully visible, warm brown wood pole with small cream cord ties, classic rally point command symbol, banner clearly readable shape filling most of the frame
```

**Cancel / Disband**
```
red X formed by two crossed steel daggers pointing downward, dark red blood-iron blades with brown leather grips, clear negation symbol, red accent breaking from faction palette intentionally
```

---

## Effects Library

All effects use the effect overlay style block and are designed to render on top of unit sprites.

### Whirlwind (64x64)
```
whirlwind tornado effect, swirling vortex in a vertical funnel shape, curved motion lines spiraling around a hollow central column, scattered leaves and dust particles caught in the wind, mostly transparent with sparse swirl lines and gaps throughout, hollow open center where the character stands, taller than wide composition, white and pale gray swirls with warm brown dust accents
```

### Explosion (64x64)
```
explosion blast effect, expanding circular fireball burst at center radiating outward, jagged warm orange and yellow flame shapes shooting in all directions, dark smoke puffs at the edges, scattered ember particles flying outward, bright cream-white hot core in the middle, sharp angular flame edges suggesting force and heat, dynamic radial composition filling the frame, warm orange yellow red and dark gray smoke palette
```

### Additional Effect Templates (To Generate As Needed)

**Fire Aura**: flame aura effect, flickering orange and yellow flames rising upward around character position, hollow center, scattered ember particles, dynamic burning visual

**Heal Sparkle**: healing magical effect, soft cream and warm yellow sparkle particles rising upward in gentle swirl, glowing motes of light, hollow center, calming radiant visual

**Shock/Lightning**: lightning shock effect, jagged electrical bolts radiating outward from center, bright cream-white core with pale yellow accents, sharp angular shapes, hollow center, electric crackling visual

**Frost/Ice**: frost magical effect, scattered ice crystal shards and pale blue mist swirling around character position, sharp crystalline particles, hollow center, cold sparkling visual

**Poison Cloud**: poison cloud effect, sickly green miasma and bubbles swirling around character position, dripping toxic droplets, hollow center, ominous murky visual

**Stun/Dizzy**: stun effect, small cream and warm yellow stars circling above the character's head position in a horizontal ring, classic comic dizzy stars, transparent everywhere except for the star particles, simple visual

**Shield/Protection**: magical shield effect, semi-transparent dome of light surrounding character position, soft glowing edge in cream and pale slate blue, hollow interior, protective barrier visual

---

## Worldbuilding Rules

These visual rules establish faction identity and narrative consistency.

### Faction Banner Discipline
- **Civic and military buildings get blue banners**: Town Hall, Barracks, Blacksmith, Tower
- **Civilian buildings do NOT get banners**: Farm
- **Neutral resource buildings do NOT get banners**: Gold Mine
- **Banner color identifies faction**: slate blue = your faction

### Faction Color Coding
- **Your main faction**: Slate blue + cream + warm brown + steel gray (disciplined military)
- **Wilderness units**: Forest green + earthy brown (rangers, scouts)
- **Druidic faction**: Deep forest green + warm brown + cream + red/white war paint (nature, rival)
- **Berserker / Wild units**: Bare skin + red warpaint + brown fur (savage, feral, breaks faction palette)

### Enemy Visual Hierarchy
Size and palette communicate threat tier instantly:
- **Pest tier** (kobold, 48x48): Dingy gray-brown, easy threat
- **Scrapper tier** (goblin, 56x56): Sickly green, common threat
- **Flying scrapper tier** (roc, 48x48): Brown raptor, aerial threat
- **Corruption tier** (vine monsters, 64-192x): Deep forest green, varies by size
- **Elite tier** (ogre 96x96): Red/black, serious threat
- **Boss tier** (ogre 168x168+, vine king 192x192): Maximum threat presence

### Unit Silhouette Test
Every unit must be recognizable by silhouette alone:
- Worker hunched with pickaxe
- Soldier upright with shield + sword
- Vanguard broad with tower shield + plumed helm + greatsword
- Berserker wild with raised battle axe
- Archer/Ranger slim with longbow at side
- Trapper hooded with crossbow at chest + visible traps
- Marksman wide-brimmed hat + longbow at side
- Caster slim with vertical staff + crystal orb
- Healer cloaked with staff + flower bloom
- Goblin small humanoid with dagger + buckler
- Kobold small rat-humanoid with candle on head
- Ogre massive brute with club on shoulder
- Roc winged predator with talons extended
- Vine monster hulking plant brute with hollow head and yellow eyes

---

## Workflow Practices

### General Generation Strategy
- Generate 6-15 variants per asset, reject most
- Buildings need 6-10 variants; units 6-8; icons 6-8 (looking for "iconic clarity")
- Lay generated assets next to existing assets to check cohesion before committing
- Keep your favorite as init image reference for related assets

### Init Image Strategy
- **Style reference only**: 0.1-0.3 strength (don't preserve shapes, only palette/tone)
- **Iterating on existing asset**: 0.5-0.7 strength
- **Small tweaks**: 0.8-0.9 strength
- **Regenerating variants**: 0.6-0.8 strength

### Color Palette Discipline
- Use PixelLab's color palette field to lock faction colors
- Strip color descriptors from prompts when using palette field (lets prompt focus on form)
- Maintain a "core palette" (6 colors) for most assets, "full palette" (10-12) for exceptions

### Cleanup Workflow in Aseprite
1. Generate at PixelLab's standard size (don't oversize source assets)
2. Open in Aseprite at 8x-16x zoom
3. Verify outlines are clean and intentional
4. Run indexed color mode to enforce palette discipline
5. For 9-slice assets: count border pixels, ensure uniform thickness, verify tileable edges
6. For terrain tiles: test by duplicating in 4x4 grid before exporting

### Terrain Tile Honest Advice
AI struggles with terrain tiles specifically. Consider buying tilesets from itch.io or OpenGameArt.org ($5-20) for grass/dirt/water and transitions. Use AI for unique units, buildings, and props where AI excels.

### Animation Strategy
- Generate base/idle frame first, lock in design
- Use base frame as init image for animation frames (0.4-0.5 strength)
- 4-8 frames typical for cycle animations
- Engine handles timing; AI provides art frames

### 9-Slice Source Asset Rules
- Border thickness uniform on all four sides
- Detail concentrated in corners (corners preserved during scaling)
- Edges (between corners) must be simple and tileable
- Center region uniform/tileable (avoid focal points)
- Use "tile" mode over "stretch" for wood/stone textures

---

## Main Menu Direction

For high-resolution main menu art beyond PixelLab's capabilities:

### Recommended Approach: Painted Illustration
Use Midjourney or Leonardo.ai for painted concept art in the style of classic 1990s RTS box art (Warcraft 2 painted loading screens, Magic: The Gathering fantasy illustration).

### Style Reference Anchors
- Classic Warcraft 2 painted box art and intro screens
- Magic: The Gathering fantasy illustration tradition
- Oil painting technique with visible painterly brushwork
- Dramatic cinematic lighting
- Atmospheric perspective (foreground sharp, background hazy)

### Composition for Menu Use
- Reserve negative space for menu UI buttons
- Position focal points off-center to allow logo placement
- Maintain faction palette and faction unit identities so menu connects to gameplay
- Stormy mood for dark fantasy framing; sunny mood for heroic fantasy framing

---

## Notes for New Conversations

When starting a new conversation with Claude (or any AI) about this project:

1. **Paste this style guide first** (or the relevant sections)
2. **Reference established decisions explicitly**: "I have an established style guide; here are the style blocks and palette I use"
3. **Cite specific units/buildings by name**: "I want a new unit that fits alongside my Vanguard, Soldier, and Marksman"
4. **Update this guide after productive sessions** to capture new decisions

Decisions live in this document. Conversations come and go.

---

*Generated from collaborative design session. Update this document as the game's visual style evolves.*