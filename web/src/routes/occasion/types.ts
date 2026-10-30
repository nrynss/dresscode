/** One occasion in the product list. The id is the slug. */
export interface OccasionSummary {
  id: string;
  name: string;
}

/** A dress-code line. The screen does not show it; the verdict does. */
export interface OccasionCriterion {
  id: string;
  kind: string;
  suggestion: string;
}

/** One fix the user can tick. `changes` is the label. */
export interface OccasionFix {
  id: string;
  changes: string;
  criteria: string[];
}

/** The dress code and the fix menu for one occasion. */
export interface OccasionDetail {
  id: string;
  name: string;
  criteria: OccasionCriterion[];
  fixes: OccasionFix[];
}

/** The ticked fixes stored for one occasion. */
export interface OptIn {
  occasion: string;
  ticked: string[];
}
