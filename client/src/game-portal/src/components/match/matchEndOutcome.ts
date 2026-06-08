/** The three outcomes the unified end-of-match recap overlay handles.
 *  Lives in its own module so both the component (MatchEndRecap.vue) and
 *  the consumer (Match.vue) can import it without depending on the SFC's
 *  internal module shape. */
export type MatchEndOutcome = 'victory' | 'defeat' | 'forfeit'
