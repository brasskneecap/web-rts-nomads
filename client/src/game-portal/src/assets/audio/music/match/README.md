# Match music

Drop `.mp3` files into this folder and they will automatically join the
in-match music rotation — no code change required.

During a match the tracks here are played in a shuffled order (no track
repeats back-to-back), advancing to the next when one ends and reshuffling
once they have all played. Volume follows the in-game music slider.

The menu loop (`Iron Warchant.mp3`) lives in the parent `music/` folder and is
**not** part of this rotation.

Discovery is handled by
[`useMatchMusic.ts`](../../../../composables/useMatchMusic.ts).
