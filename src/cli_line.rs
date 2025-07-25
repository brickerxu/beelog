use beelog::config;
use chrono::Local;
use reedline::{FileBackedHistory, Prompt, PromptEditMode, PromptHistorySearch, Reedline};
use std::borrow::Cow;

pub struct CliLine {
    pub line_editor: Reedline,
    pub prompt: CustomPrompt,
}

impl CliLine {
    
    pub fn new(left_prompt: &String) -> Self {
        let history_path = config::get_history_path();
        let history = Box::new(
            FileBackedHistory::with_file(50, history_path.into())
                .map_err(|e| format!("历史文件读取异常: {}", e))
                .unwrap(),
        );
        let line_editor = Reedline::create()
            .with_history(history);
        let prompt = CustomPrompt::new(left_prompt.clone());
        Self {
            line_editor,
            prompt,
        }
    }
}

pub struct CustomPrompt {
    left_prompt: String,
}

impl CustomPrompt {
    fn new(left_prompt: String) -> Self {
        
        Self {
            left_prompt,
        }
    }

}

impl Prompt for CustomPrompt {
    
    fn render_prompt_left(&self) -> Cow<str> {
        Cow::Borrowed(&self.left_prompt)
    }

    fn render_prompt_right(&self) -> Cow<str> {
        Cow::Owned(get_now())
    }

    fn render_prompt_indicator(&self, _: PromptEditMode) -> Cow<str> {
        Cow::Borrowed(" >> ")
    }

    fn render_prompt_multiline_indicator(&self) -> Cow<str> {
        Cow::Borrowed("render_prompt_multiline_indicator")
    }

    fn render_prompt_history_search_indicator(&self, _: PromptHistorySearch) -> Cow<str> {
        Cow::Borrowed("render_prompt_history_search_indicator")
    }
    
}

fn get_now() -> String {
    let now = Local::now();
    format!("{:>}", now.format("%Y/%m/%d %H:%M:%S"))
}