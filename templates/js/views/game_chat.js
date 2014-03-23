window.GameChatView = BaseView.extend({

  template: _.template($('#game_chat_underscore').html()),

	events: {
	  "click .create-channel-button": "createChannel",
		"shown.bs.collapse .channel": "channelShow",
	},

	initialize: function() {
	  this.channels = {};
	  this.listenTo(this.collection, 'add', this.addMessage);
	  this.listenTo(this.collection, 'reset', this.loadMessages);
	},

	channelShow: function(ev) {
		window.session.router.navigate('/games/' + this.model.get('Id') + '/messages/' + $(ev.target).attr('data-participants'), { trigger: false });
	},

	loadMessages: function() {
	  var that = this;
		that.$('#chat-channels').empty();
		that.collection.each(function(message) {
		  if (message.get('SenderId') != null) {
				that.addMessage(message);
			}
		});
	},

	ensureChannel: function(members) {
	  var that = this;
		var channelId = ChatMessage.channelIdFor(members);
		if (that.channels[channelId] == null) {
			var newChannelView = new ChatChannelView({
				collection: that.collection,
				model: that.model,
				members: members,
			}).doRender();
			that.channels[channelId] = newChannelView;
			that.$('#chat-channels').append(newChannelView.el);
		}
		return channelId;
	},

	addMessage: function(message) {
		var that = this;
		var channelId = that.ensureChannel(message.get('Recipients'));
		that.channels[channelId].$('.chat-messages').prepend(new ChatMessageView({
			model: message,
			game: that.model,
		}).doRender().el);
	},

	createChannel: function() {
	  var that = this;
	  var memberNations = that.$('.new-channel-nations').val().sort();
		memberNations.push(that.model.me().Nation);
		if (that.model.allowChatMembers(memberNations.length)) {
		  members = _.inject(memberNations, function(sum, nat) {
			  sum[nat] = true;
				return sum;
			}, {});
			that.ensureChannel(members);
		} else {
			that.$('.create-channel-container').append('<div class="alert alert-warning fade in">' + 
				'<button type="button" class="close" data-dismiss="alert" aria-hidden="true">&times;</button>' + 
				'<strong>' +
				'{{.I "The game does not allow that particular number of members in a chat channel right now. The only types of chat allowed at the moment are {0}."}}'.format(that.model.describeCurrentChatFlagOptions()) +
				'</strong>' + 
			'</div>');
		}
	},

	disableSelector: function() {
	  var that = this;
		var sel = that.$('.new-channel-nations');
		var selectedOptions = sel.find('option:selected');
		var nonSelectedOptions = sel.find('option').filter(function() {
			return !$(this).is(':selected');
		});
		var dropdown = sel.parent().find('.multiselect-container');

		nonSelectedOptions.each(function() {
			var input = dropdown.find('input[value="' + $(this).val() + '"]');
			input.prop('disabled', true);
			input.parent('li').addClass('disabled');
		});
	},

	enableSelector: function() {
		var that = this;
		var sel = that.$('.new-channel-nations');
		var dropdown = sel.parent().find('.multiselect-container');

		sel.find('option').each(function() {
			var input = dropdown.find('input[value="' + $(this).val() + '"]');
			input.prop('disabled', false);
			input.parent('li').addClass('disabled');
		});
	},

  render: function() {
	  var that = this;
	  that.channels = {};
    that.$el.html(that.template({
		}));
		var me = that.model.me();
		if (me == null) {
		  that.$('.create-channel-container').hide();
		} else {
			_.each(that.model.members(), function(member) {
			  if (member.Id != me.Id) {
					var opt = $('<option value="' + member.Nation + '"></option>');
					opt.text(member.Nation);
					that.$('.new-channel-nations').append(opt);
				}
			});
      var opts = {
				onDropdownHide: function(ev) {
					var el = $(ev.currentTarget);
					el.css('margin-bottom', 0);
				},
				onDropdownShow: function(ev) {
					var el = $(ev.currentTarget);
					el.css('margin-bottom', el.find('.multiselect-container').height());
				},
			};
			that.$('.new-channel-nations').multiselect(opts);
		}
		if (this.model.get('Phase') != null) {
			this.loadMessages();
		}
		return that;
	},

});
