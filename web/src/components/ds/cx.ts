export const cx = (...xs: Array<string | false | undefined | null>) =>
  xs.filter(Boolean).join(" ");
