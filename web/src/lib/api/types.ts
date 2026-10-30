/** One recorded response from testdata/api. Error files have no method or path. */
export interface ApiFixture {
  file: string;
  method?: string;
  path?: string;
  status?: number;
  headers?: Record<string, string>;
  body?: unknown;
  where?: string;
}

/** A wire error the screen can say in a sentence. `code` is what we branch on. */
export interface ShownError {
  code: string;
  message?: string;
  detail?: unknown;
  status?: number;
}
