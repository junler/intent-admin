export function validCron(value: string): boolean {
	const input = value.trim();
	if (input.startsWith("@every ")) {
		const duration = input.slice(7).trim();
		if (!/^(?:\d+(?:\.\d+)?(?:ms|s|m|h))+$/.test(duration)) return false;
		let ms = 0;
		for (const match of duration.matchAll(/(\d+(?:\.\d+)?)(ms|s|m|h)/g)) {
			ms +=
				Number(match[1]) *
				({ ms: 1, s: 1000, m: 60000, h: 3600000 }[match[2]] ?? 0);
		}
		return ms >= 1000;
	}
	return (
		!input.includes("?") &&
		input.split(/\s+/).length === 5 &&
		input.split(/\s+/).every((part) => /^[\d*,/\-A-Za-z]+$/.test(part))
	);
}
