// Joins two transcript segments. Desktop Chrome prefixes follow-up results
// with a space, but mobile engines (Android Chrome, iOS Safari) and the local
// Kroko recognizer usually don't, which glued words together across result
// boundaries ("directoryand").
export function joinTranscript(a: string, b: string): string {
	if (!a || !b) return a + b
	if (/\s$/.test(a) || /^\s/.test(b)) return a + b
	return `${a} ${b}`
}
