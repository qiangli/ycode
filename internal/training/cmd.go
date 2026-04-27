package training

// TrainConfig holds configuration for training subcommands.
type TrainConfig struct {
	Task        string // task name (gsm8k, terminal, humaneval, etc.)
	Model       string // model path or name
	OutputDir   string // output directory
	Steps       int    // total training steps
	BatchSize   int    // batch size
	Concurrency int    // parallel rollouts for data collection
	Resume      bool   // resume from checkpoint
}

// CollectConfig holds configuration for trajectory collection.
type CollectConfig struct {
	Task        string // task name
	Model       string // model for inference
	OutputPath  string // JSONL output file
	Count       int    // number of trajectories to collect
	Concurrency int    // parallel rollouts
}

// EvalConfig holds configuration for model evaluation.
type EvalConfig struct {
	Task    string // task name
	Model   string // model to evaluate
	Samples int    // number of examples to evaluate
}
