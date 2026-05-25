export interface UAInfo {
  browser: string;
  os: string;
}

export function parseUserAgent(ua: string): UAInfo {
  if (!ua) return { browser: "Unknown", os: "Unknown" };

  let os = "Unknown";
  if (/Windows/i.test(ua)) os = "Windows";
  else if (/Mac OS X|Macintosh/i.test(ua)) os = "macOS";
  else if (/Android/i.test(ua)) os = "Android";
  else if (/iPhone|iPad|iOS/i.test(ua)) os = "iOS";
  else if (/Linux/i.test(ua)) os = "Linux";

  let browser = "Unknown";
  const edge = ua.match(/Edg\/(\d+)/);
  const chrome = ua.match(/Chrome\/(\d+)/);
  const firefox = ua.match(/Firefox\/(\d+)/);
  const safariV = ua.match(/Version\/(\d+).*Safari\//);
  const curl = ua.match(/^curl\/(\S+)/);
  if (edge) browser = `Edge ${edge[1]}`;
  else if (firefox) browser = `Firefox ${firefox[1]}`;
  else if (safariV) browser = `Safari ${safariV[1]}`;
  else if (chrome) browser = `Chrome ${chrome[1]}`;
  else if (curl) browser = `curl ${curl[1]}`;

  return { browser, os };
}
